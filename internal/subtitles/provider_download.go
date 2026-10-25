package subtitles

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PublishedStage is an unverified, isolated candidate. It is not an install
// authorization: release identity, timing and dialogue still need review.
type PublishedStage struct {
	Provider       string `json:"provider"`
	FileID         int64  `json:"fileId"`
	StageID        string `json:"stageId"`
	OutputSRT      string `json:"-"`
	SHA256         string `json:"sha256"`
	Audit          Audit  `json:"audit"`
	ReadyForReview bool   `json:"readyForReview"`
}

// StageOpenSubtitles fetches one explicitly selected subtitle into a private
// staging directory. It never publishes to the media directory. The API-token
// request and the temporary file GET are separate; provider credentials are
// sent only to the fixed API endpoint, never to the returned link.
func StageOpenSubtitles(ctx context.Context, client *http.Client, apiKey, bearer string, fileID int64, partID, video, stageRoot string, durationMS int64) (PublishedStage, error) {
	if ctx == nil {
		return PublishedStage{}, errors.New("missing context")
	}
	if err := ctx.Err(); err != nil {
		return PublishedStage{}, err
	}
	if fileID <= 0 || fileID > 2147483647 || partID == "" || durationMS <= 0 ||
		!filepath.IsAbs(video) || !filepath.IsAbs(stageRoot) ||
		len(apiKey) == 0 || len(apiKey) > 256 || strings.ContainsAny(apiKey, "\r\n") ||
		len(bearer) == 0 || len(bearer) > 4096 || strings.ContainsAny(bearer, "\r\n") {
		return PublishedStage{}, errors.New("invalid subtitle download configuration")
	}
	info, err := regularFile(video, 0)
	if err != nil {
		return PublishedStage{}, fmt.Errorf("reference media rejected: %w", err)
	}
	rootInfo, err := os.Lstat(stageRoot)
	if err != nil {
		return PublishedStage{}, errors.New("staging root unavailable")
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return PublishedStage{}, errors.New("staging root must be a regular directory")
	}
	if err := stageOutsideMedia(video, stageRoot); err != nil {
		return PublishedStage{}, err
	}
	videoFingerprint, err := fingerprintMediaPath(video)
	if err != nil {
		return PublishedStage{}, errors.New("cannot fingerprint reference media")
	}
	// Pin the validated directory *before* network I/O. A writable ancestor
	// cannot redirect a later stage write to a different root during download.
	stageDir, err := os.OpenRoot(stageRoot)
	if err != nil {
		return PublishedStage{}, errors.New("cannot open private subtitle staging directory")
	}
	defer stageDir.Close()
	openedRootInfo, err := stageDir.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		return PublishedStage{}, errors.New("subtitle staging root changed")
	}

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	transportClient := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if client != nil {
		transportClient.Transport = client.Transport
	}
	requestBody, _ := json.Marshal(map[string]any{"file_id": fileID, "sub_format": "srt"})
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, "https://api.opensubtitles.com/api/v1/download", bytes.NewReader(requestBody))
	if err != nil {
		return PublishedStage{}, errors.New("invalid provider request")
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("User-Agent", "Mediaphile v1.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := transportClient.Do(req)
	if err != nil {
		if runCtx.Err() != nil {
			return PublishedStage{}, runCtx.Err()
		}
		return PublishedStage{}, errors.New("provider download request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return PublishedStage{}, ErrSubtitleProviderRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return PublishedStage{}, fmt.Errorf("provider download request returned HTTP %d", resp.StatusCode)
	}
	metadata, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil || len(metadata) > 8192 {
		return PublishedStage{}, errors.New("provider download metadata invalid")
	}
	var ticket struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(metadata, &ticket); err != nil {
		return PublishedStage{}, errors.New("provider download metadata invalid")
	}
	link, err := validateSubtitleDownloadURL(ticket.Link)
	if err != nil {
		return PublishedStage{}, err
	}
	getReq, err := http.NewRequestWithContext(runCtx, http.MethodGet, link.String(), nil)
	if err != nil {
		return PublishedStage{}, errors.New("invalid provider file request")
	}
	getReq.Header.Set("User-Agent", "Mediaphile v1.0")
	getResp, err := transportClient.Do(getReq)
	if err != nil {
		if runCtx.Err() != nil {
			return PublishedStage{}, runCtx.Err()
		}
		return PublishedStage{}, errors.New("provider file retrieval failed")
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		return PublishedStage{}, fmt.Errorf("provider file retrieval returned HTTP %d", getResp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(getResp.Body, maxSRTBytes+1))
	if err != nil || len(payload) > maxSRTBytes {
		return PublishedStage{}, errors.New("provider subtitle exceeds size limit or could not be read")
	}
	report, err := AuditSRT(payload, durationMS)
	if err != nil {
		return PublishedStage{}, fmt.Errorf("downloaded subtitle failed structural audit: %w", err)
	}
	if err := runCtx.Err(); err != nil {
		return PublishedStage{}, err
	}
	current, err := os.Stat(video)
	if err != nil || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return PublishedStage{}, errors.New("reference media changed during staging")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return PublishedStage{}, errors.New("cannot create staging identifier")
	}
	stageID := hex.EncodeToString(nonce[:])
	workName := "published-subtitle-" + stageID
	if err := stageDir.Mkdir(workName, 0700); err != nil {
		return PublishedStage{}, errors.New("cannot create subtitle staging directory")
	}
	workInfo, err := stageDir.Lstat(workName)
	if err != nil || !workInfo.IsDir() || workInfo.Mode()&os.ModeSymlink != 0 {
		return PublishedStage{}, errors.New("subtitle stage directory replaced")
	}
	success := false
	defer func() {
		if !success {
			_ = stageDir.RemoveAll(workName)
		}
	}()
	workRoot, err := stageDir.OpenRoot(workName)
	if err != nil {
		return PublishedStage{}, errors.New("cannot open isolated staging directory")
	}
	defer workRoot.Close()
	openedWorkInfo, err := workRoot.Stat(".")
	if err != nil || !os.SameFile(workInfo, openedWorkInfo) {
		return PublishedStage{}, errors.New("subtitle stage directory changed")
	}
	output := filepath.Join(stageRoot, workName, "candidate.en.srt")
	f, err := workRoot.OpenFile("candidate.en.srt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return PublishedStage{}, errors.New("cannot create staged subtitle")
	}
	n, writeErr := f.Write(payload)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || n != len(payload) {
		return PublishedStage{}, errors.New("cannot finish staged subtitle")
	}
	if err := runCtx.Err(); err != nil {
		return PublishedStage{}, err
	}
	digest := sha256.Sum256(payload)
	manifest := publishedStageManifest{Version: 1, PartID: partID, Video: filepath.Clean(video),
		VideoSize: info.Size(), VideoModTimeNano: info.ModTime().UnixNano(), DurationMS: durationMS,
		VideoFingerprint: videoFingerprint,
		Provider: "opensubtitles", FileID: fileID, SHA256: hex.EncodeToString(digest[:]),
		CreatedAtUnix: time.Now().Unix()}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return PublishedStage{}, errors.New("cannot encode subtitle stage manifest")
	}
	manifestFile, err := workRoot.OpenFile("manifest.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return PublishedStage{}, errors.New("cannot create subtitle stage manifest")
	}
	n, writeErr = manifestFile.Write(manifestBytes)
	syncErr = manifestFile.Sync()
	closeErr = manifestFile.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || n != len(manifestBytes) {
		return PublishedStage{}, errors.New("cannot finish subtitle stage manifest")
	}
	success = true
	return PublishedStage{Provider: "opensubtitles", FileID: fileID, StageID: stageID, OutputSRT: output,
		SHA256: hex.EncodeToString(digest[:]), Audit: report, ReadyForReview: true}, nil
}

func validateSubtitleDownloadURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, errors.New("provider returned invalid download link")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() != "www.opensubtitles.com" ||
		(u.Port() != "" && u.Port() != "443") || u.User != nil || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, "/download/") || len(u.Path) < len("/download/x") {
		return nil, errors.New("provider returned untrusted download link")
	}
	return u, nil
}
