package subtitles

import (
	"crypto/sha256"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A manifest is created only by the server's provider staging path. Client
// requests identify it by an opaque nonce, never by an arbitrary filesystem path.
type publishedStageManifest struct {
	Version          int    `json:"version"`
	PartID           string `json:"partId"`
	Video            string `json:"video"`
	VideoSize        int64  `json:"videoSize"`
	VideoModTimeNano int64  `json:"videoModTimeNano"`
	VideoFingerprint string `json:"videoFingerprint"`
	DurationMS       int64  `json:"durationMs"`
	Provider         string `json:"provider"`
	FileID           int64  `json:"fileId"`
	SHA256           string `json:"sha256"`
	CreatedAtUnix    int64  `json:"createdAtUnix"`
}

type PublishedReview struct {
	PartID         string `json:"partId"`
	StageID        string `json:"stageId"`
	Provider       string `json:"provider"`
	FileID         int64  `json:"fileId"`
	SHA256         string `json:"sha256"`
	Audit          Audit  `json:"audit"`
	Text           string `json:"text"`
	ReadyForReview bool   `json:"readyForReview"`
	Published      bool   `json:"published"`
}

type PublishedInstall struct {
	DestinationName string `json:"destinationName"`
	SHA256          string `json:"sha256"`
	Bytes           int64  `json:"bytes"`
	Source          string `json:"source"`
	AlreadyInstalled bool `json:"alreadyInstalled"`
}

var publishedStageIDRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
var publishedSHA256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// An explicitly scoped publication may be readable by a separate playback
// account. Keep staging at 0600; permit only these non-executable, non-writable-
// by-other final modes, with a readable default for Plex and similar services.
// Chmod is explicit so the process umask does not silently make the final
// hardlinked sidecar unreadable to a different service account.
func publishedSubtitleMode() (os.FileMode, error) {
	switch os.Getenv("MEDIAPHILE_SUBTITLE_PUBLISH_MODE") {
	case "", "0644":
		return 0644, nil
	case "0640":
		return 0640, nil
	case "0600":
		return 0600, nil
	default:
		return 0, errors.New("unsafe subtitle publication mode")
	}
}

// openPublishedStage binds a staged candidate to the indexed media part and
// returns immutable, digest-checked bytes. os.Root confines all relative reads
// even when a directory is replaced while a request is in flight.
func openPublishedStage(stageRoot, partID, video string, durationMS int64, stageID string) (publishedStageManifest, []byte, Audit, error) {
	videoInfo, err := regularFile(video, 0)
	if err != nil {
		return publishedStageManifest{}, nil, Audit{}, errors.New("reference media unavailable")
	}
	return openPublishedStageWithMediaInfo(stageRoot, partID, video, durationMS, stageID, videoInfo, "")
}

// The API-facing variant passes media information captured through a pinned
// library-root handle. No unrooted media path is opened in this function.
func openPublishedStageWithMediaInfo(stageRoot, partID, video string, durationMS int64, stageID string, videoInfo os.FileInfo, libraryRoot string) (publishedStageManifest, []byte, Audit, error) {
	var empty publishedStageManifest
	if !publishedStageIDRE.MatchString(stageID) || partID == "" || !filepath.IsAbs(stageRoot) || !filepath.IsAbs(video) || durationMS <= 0 {
		return empty, nil, Audit{}, errors.New("invalid staged subtitle reference")
	}
	if videoInfo == nil || !videoInfo.Mode().IsRegular() || videoInfo.Size() < 1 {
		return empty, nil, Audit{}, errors.New("reference media unavailable")
	}
	rootInfo, err := os.Lstat(stageRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return empty, nil, Audit{}, errors.New("private staging root unavailable")
	}
	if err := stageOutsideMedia(video, stageRoot); err != nil {
		return empty, nil, Audit{}, errors.New("staging root is not isolated from media")
	}
	root, err := os.OpenRoot(stageRoot)
	if err != nil {
		return empty, nil, Audit{}, errors.New("private staging root unavailable")
	}
	defer root.Close()
	openedRootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		return empty, nil, Audit{}, errors.New("private staging root changed")
	}
	if err := verifyLibraryStageRoot(libraryRoot, stageRoot, openedRootInfo); err != nil {
		return empty, nil, Audit{}, errors.New("staging root changed or is not isolated from media library")
	}
	folder := "published-subtitle-" + stageID
	directoryInfo, err := root.Lstat(folder)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return empty, nil, Audit{}, errors.New("staged subtitle directory unavailable")
	}
	stage, err := root.OpenRoot(folder)
	if err != nil {
		return empty, nil, Audit{}, errors.New("staged subtitle directory unavailable")
	}
	defer stage.Close()
	actualDirectoryInfo, err := stage.Stat(".")
	if err != nil || !os.SameFile(directoryInfo, actualDirectoryInfo) {
		return empty, nil, Audit{}, errors.New("staged subtitle directory changed")
	}
	readPinned := func(name string, limit int64) ([]byte, error) {
		before, err := stage.Lstat(name)
		if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > limit {
			return nil, errors.New("staged file rejected")
		}
		file, err := stage.Open(name)
		if err != nil {
			return nil, errors.New("staged file unavailable")
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
			return nil, errors.New("staged file replaced or linked")
		}
		bytes, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil || len(bytes) == 0 || int64(len(bytes)) > limit {
			return nil, errors.New("staged file exceeds size limit")
		}
		return bytes, nil
	}
	manifestBytes, err := readPinned("manifest.json", 4096)
	if err != nil {
		return empty, nil, Audit{}, err
	}
	var manifest publishedStageManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return empty, nil, Audit{}, errors.New("staged manifest invalid")
	}
	if manifest.Version != 1 || manifest.PartID != partID || manifest.Video != filepath.Clean(video) ||
		manifest.DurationMS != durationMS || manifest.VideoSize != videoInfo.Size() ||
		manifest.VideoModTimeNano != videoInfo.ModTime().UnixNano() ||
		manifest.Provider != "opensubtitles" || manifest.FileID <= 0 ||
		manifest.CreatedAtUnix <= 0 || manifest.CreatedAtUnix > time.Now().Add(5*time.Minute).Unix() ||
		time.Now().Unix()-manifest.CreatedAtUnix > int64((30*24*time.Hour)/time.Second) ||
		!publishedSHA256RE.MatchString(manifest.VideoFingerprint) ||
		!publishedSHA256RE.MatchString(manifest.SHA256) {
		return empty, nil, Audit{}, errors.New("staged subtitle does not match this media part")
	}
	payload, err := readPinned("candidate.en.srt", maxSRTBytes)
	if err != nil {
		return empty, nil, Audit{}, err
	}
	if !utf8.Valid(payload) {
		return empty, nil, Audit{}, errors.New("staged subtitle is not UTF-8")
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != manifest.SHA256 {
		return empty, nil, Audit{}, errors.New("staged subtitle digest changed")
	}
	audit, err := AuditSRT(payload, durationMS)
	if err != nil {
		return empty, nil, Audit{}, errors.New("staged subtitle failed structural audit")
	}
	return manifest, payload, audit, nil
}

// ReviewPublishedStage is read-only, exposing at most 2 MiB of bounded SRT
// dialogue to an authenticated admin. This is not an install authorization.
func ReviewPublishedStage(stageRoot, partID, video string, durationMS int64, stageID string) (PublishedReview, error) {
	manifest, payload, audit, err := openPublishedStage(stageRoot, partID, video, durationMS, stageID)
	if err != nil {
		return PublishedReview{}, err
	}
	return PublishedReview{PartID: partID, StageID: stageID, Provider: manifest.Provider,
		FileID: manifest.FileID, SHA256: manifest.SHA256, Audit: audit, Text: string(payload),
		ReadyForReview: true, Published: false}, nil
}

// ReviewPublishedStageInLibrary rejects an outside-library or stale media
// reference before disclosing even bounded staged subtitle text.
func ReviewPublishedStageInLibrary(libraryRoot, stageRoot, partID, video string, size, modTimeNS, durationMS int64, stageID string) (PublishedReview, error) {
	if err := stageOutsideDirectory(libraryRoot, stageRoot); err != nil {
		return PublishedReview{}, errors.New("staging root is not outside the media library")
	}
	mediaRoot, err := openLibraryMediaRoot(libraryRoot, video, size, modTimeNS)
	if err != nil {
		return PublishedReview{}, err
	}
	defer mediaRoot.Close()
	mediaInfo, err := mediaRoot.Lstat(filepath.Base(video))
	if err != nil {
		return PublishedReview{}, err
	}
	manifest, payload, audit, err := openPublishedStageWithMediaInfo(stageRoot, partID, video, durationMS, stageID, mediaInfo, libraryRoot)
	if err != nil {
		return PublishedReview{}, err
	}
	return PublishedReview{PartID: partID, StageID: stageID, Provider: manifest.Provider,
		FileID: manifest.FileID, SHA256: manifest.SHA256, Audit: audit, Text: string(payload),
		ReadyForReview: true, Published: false}, nil
}

// InstallPublishedStage is invoked exclusively by the explicit admin-confirmed
// endpoint. It never changes the media file or overwrites an existing sidecar.
func InstallPublishedStage(stageRoot, partID, video string, durationMS int64, stageID, expectedSHA256 string) (PublishedInstall, error) {
	parentInfo, err := os.Lstat(filepath.Dir(video))
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return PublishedInstall{}, errors.New("media directory is not a regular directory")
	}
	mediaRoot, err := os.OpenRoot(filepath.Dir(video))
	if err != nil {
		return PublishedInstall{}, errors.New("media directory unavailable")
	}
	defer mediaRoot.Close()
	openedParentInfo, err := mediaRoot.Stat(".")
	if err != nil || !os.SameFile(parentInfo, openedParentInfo) {
		return PublishedInstall{}, errors.New("media directory changed")
	}
	return installPublishedRoot(mediaRoot, stageRoot, partID, video, durationMS, stageID, expectedSHA256, "")
}

// InstallPublishedStageInLibrary holds the library-confined media directory
// open across stage revalidation, content fingerprinting, and atomic publish.
func InstallPublishedStageInLibrary(libraryRoot, stageRoot, partID, video string, size, modTimeNS, durationMS int64, stageID, expectedSHA256 string) (PublishedInstall, error) {
	if err := stageOutsideDirectory(libraryRoot, stageRoot); err != nil {
		return PublishedInstall{}, errors.New("staging root is not outside the media library")
	}
	mediaRoot, err := openLibraryMediaRoot(libraryRoot, video, size, modTimeNS)
	if err != nil {
		return PublishedInstall{}, err
	}
	defer mediaRoot.Close()
	return installPublishedRoot(mediaRoot, stageRoot, partID, video, durationMS, stageID, expectedSHA256, libraryRoot)
}

func installPublishedRoot(mediaRoot *os.Root, stageRoot, partID, video string, durationMS int64, stageID, expectedSHA256, libraryRoot string) (PublishedInstall, error) {
	if !publishedSHA256RE.MatchString(expectedSHA256) {
		return PublishedInstall{}, errors.New("invalid expected SHA256")
	}
	videoName := filepath.Base(video)
	mediaInfo, err := mediaRoot.Lstat(videoName)
	if err != nil || !mediaInfo.Mode().IsRegular() {
		return PublishedInstall{}, errors.New("reference media unavailable")
	}
	manifest, payload, audit, err := openPublishedStageWithMediaInfo(stageRoot, partID, video, durationMS, stageID, mediaInfo, libraryRoot)
	if err != nil {
		return PublishedInstall{}, err
	}
	if manifest.SHA256 != expectedSHA256 || audit.Status != "ready" {
		return PublishedInstall{}, errors.New("staged subtitle changed or has unresolved review findings")
	}
	publicationMode, err := publishedSubtitleMode()
	if err != nil {
		return PublishedInstall{}, err
	}
	videoInfo, err := mediaRoot.Lstat(videoName)
	if err != nil || !videoInfo.Mode().IsRegular() || videoInfo.Size() != manifest.VideoSize ||
		videoInfo.ModTime().UnixNano() != manifest.VideoModTimeNano {
		return PublishedInstall{}, errors.New("reference media changed")
	}
	videoFile, err := mediaRoot.Open(videoName)
	if err != nil {
		return PublishedInstall{}, errors.New("reference media unavailable")
	}
	openedVideoInfo, err := videoFile.Stat()
	if err != nil || !os.SameFile(videoInfo, openedVideoInfo) {
		videoFile.Close()
		return PublishedInstall{}, errors.New("reference media replaced")
	}
	fingerprint, err := fingerprintOpenedMedia(videoFile, openedVideoInfo.Size())
	videoFile.Close()
	if err != nil || fingerprint != manifest.VideoFingerprint {
		return PublishedInstall{}, errors.New("reference media content fingerprint changed")
	}
	destinationName := strings.TrimSuffix(videoName, filepath.Ext(videoName)) + ".en.srt"
	result := PublishedInstall{DestinationName: destinationName,
		SHA256: manifest.SHA256, Bytes: int64(len(payload)), Source: "opensubtitles:" + fmt.Sprint(manifest.FileID)}
	// The hard-link publish is atomic and fails if an existing sidecar exists.
	// Every byte is written and synced to a private random temporary name first.
	// On filesystems that cannot provide this guarantee, fail closed.
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return PublishedInstall{}, errors.New("cannot create private publication identifier")
	}
	tempName := ".mediaphile-subtitle-" + hex.EncodeToString(nonce[:]) + ".tmp"
	temp, err := mediaRoot.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return PublishedInstall{}, errors.New("cannot create temporary subtitle")
	}
	defer mediaRoot.Remove(tempName)
	n, writeErr := temp.Write(payload)
	modeErr := temp.Chmod(publicationMode)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if writeErr != nil || modeErr != nil || syncErr != nil || closeErr != nil || n != len(payload) {
		return PublishedInstall{}, errors.New("temporary subtitle write failed without publishing")
	}
	tempInfo, err := mediaRoot.Lstat(tempName)
	if err != nil || !tempInfo.Mode().IsRegular() || tempInfo.Size() != int64(len(payload)) {
		return PublishedInstall{}, errors.New("temporary subtitle replaced")
	}
	verify, err := mediaRoot.Open(tempName)
	if err != nil {
		return PublishedInstall{}, errors.New("cannot verify temporary subtitle")
	}
	openedTempInfo, err := verify.Stat()
	if err != nil || !os.SameFile(tempInfo, openedTempInfo) {
		verify.Close()
		return PublishedInstall{}, errors.New("temporary subtitle replaced")
	}
	check, readErr := io.ReadAll(io.LimitReader(verify, maxSRTBytes+1))
	verify.Close()
	checkSHA := sha256.Sum256(check)
	if readErr != nil || hex.EncodeToString(checkSHA[:]) != expectedSHA256 {
		return PublishedInstall{}, errors.New("temporary subtitle content mismatch")
	}
	if err := mediaRoot.Link(tempName, destinationName); err != nil {
		if !os.IsExist(err) {
			return PublishedInstall{}, errors.New("atomic no-overwrite publish unavailable")
		}
		// A concurrent installation can only be idempotent once the winner's
		// fully synced bytes become visible via the atomic link.
		if !publishedSidecarMatches(mediaRoot, destinationName, expectedSHA256, int64(len(payload))) {
			return PublishedInstall{}, errors.New("different subtitle sidecar already exists")
		}
		result.AlreadyInstalled = true
		result.Source = "existing_identical_unattributed"
		return result, nil
	}
	if !publishedSidecarMatches(mediaRoot, destinationName, expectedSHA256, int64(len(payload))) {
		return PublishedInstall{}, errors.New("published subtitle failed readback")
	}
	return result, nil
}

// Bounded fingerprint detects ordinary same-size/mtime media replacement
// without hashing multi-gigabyte videos on the NAS. It is NOT a full-content
// identity proof: deliberate changes outside the sampled regions can evade it.
func fingerprintOpenedMedia(file *os.File, size int64) (string, error) {
	if size <= 0 {
		return "", errors.New("invalid reference media size")
	}
	const window int64 = 64 * 1024
	h := sha256.New()
	_, _ = h.Write([]byte("mediaphile-media-samples-v1:" + strconv.FormatInt(size, 10)))
	for _, offset := range []int64{0, size / 2, size - window} {
		if offset < 0 {
			offset = 0
		}
		length := window
		if size-offset < length {
			length = size - offset
		}
		chunk := make([]byte, int(length))
		if _, err := file.ReadAt(chunk, offset); err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(":" + strconv.FormatInt(offset, 10) + ":"))
		_, _ = h.Write(chunk)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fingerprintMediaPath(video string) (string, error) {
	before, err := regularFile(video, 0)
	if err != nil {
		return "", err
	}
	f, err := os.Open(video)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return "", errors.New("reference media changed")
	}
	return fingerprintOpenedMedia(f, opened.Size())
}

func publishedSidecarMatches(root *os.Root, name, expectedSHA string, expectedSize int64) bool {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != expectedSize {
		return false
	}
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSRTBytes+1))
	if err != nil || int64(len(data)) != expectedSize {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == expectedSHA
}
