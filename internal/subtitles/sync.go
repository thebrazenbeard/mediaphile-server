package subtitles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SyncRequest is a source-only staging request; the caller must supply a
// separate, trusted staging root outside the media directory.
type SyncRequest struct {
	Video      string
	InputSRT   string
	StageRoot  string
	Binary     string
	DurationMS int64
}

// SyncRunner is injectable for tests. The production runner executes the
// fixed binary and explicit argument vector directly without invoking a shell.
type SyncRunner func(context.Context, string, ...string) error

type SyncStage struct {
	OutputSRT      string `json:"outputSrt"`
	InputSHA256    string `json:"inputSha256"`
	OutputSHA256   string `json:"outputSha256"`
	Audit          Audit  `json:"audit"`
	ReadyForReview bool   `json:"readyForReview"`
}

// ExecFFsubsync runs a configured ffsubsync executable without a shell.
// Caller-controlled arguments are never interpreted as executable commands.
func ExecFFsubsync(ctx context.Context, binary string, args ...string) error {
	if !filepath.IsAbs(binary) {
		return errors.New("ffsubsync executable must be an explicit absolute path")
	}
	if _, err := regularFile(binary, 0); err != nil {
		return fmt.Errorf("ffsubsync executable rejected: %w", err)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	// Capture only bounded diagnostics to prevent verbose external tools from
	// exhausting the server's memory. The writer acknowledges excess bytes.
	sink := &syncDiagnosticBuffer{max: 4096}
	cmd.Stdout = sink
	cmd.Stderr = sink
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffsubsync execution failed: %w: %s", err, sink.String())
	}
	return nil
}

// syncDiagnosticBuffer is a bounded combined output sink.
type syncDiagnosticBuffer struct {
	data bytes.Buffer
	max  int
}

func (d *syncDiagnosticBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := d.max - d.data.Len()
	if remaining > 0 {
		if remaining > n {
			remaining = n
		}
		_, _ = d.data.Write(p[:remaining])
	}
	return n, nil
}
func (d *syncDiagnosticBuffer) String() string { return d.data.String() }
func regularFile(path string, maxSize int64) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("regular non-symlink file required")
	}
	if info.Size() < 1 || (maxSize > 0 && info.Size() > maxSize) {
		return nil, errors.New("file size outside permitted bounds")
	}
	return info, nil
}

func stageOutsideMedia(video, root string) error {
	return stageOutsideDirectory(filepath.Dir(video), root)
}

// stageOutsideDirectory also guards the full catalog library root on
// API-facing routes. Checking only a video's immediate parent would allow
// staging in a sibling directory that the media library indexes.
func stageOutsideDirectory(mediaDir, root string) error {
	parent, err := filepath.EvalSymlinks(mediaDir)
	if err != nil {
		return err
	}
	cleanRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	mediaToStage, err := filepath.Rel(parent, cleanRoot)
	if err != nil {
		return err
	}
	stageToMedia, err := filepath.Rel(cleanRoot, parent)
	if err != nil {
		return err
	}
	contained := func(relative string) bool {
		return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
	}
	if contained(mediaToStage) || contained(stageToMedia) {
		return errors.New("staging must be outside media directory")
	}
	return nil
}

// Revalidate an already-opened staging root against the configured library
// immediately before a sensitive operation. An ancestor replacement after
// the first pathname check must not silently select a different directory.
// This is not a substitute for a trusted, non-relocatable staging volume.
func verifyLibraryStageRoot(libraryRoot, stageRoot string, opened os.FileInfo) error {
	if libraryRoot == "" {
		return nil // local non-catalog staging API has no library-root context
	}
	if err := stageOutsideDirectory(libraryRoot, stageRoot); err != nil {
		return err
	}
	current, err := os.Lstat(stageRoot)
	if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 ||
		opened == nil || !os.SameFile(current, opened) {
		return errors.New("staging root changed")
	}
	return nil
}

// StageSync invokes ffsubsync with explicit input and output. All produced
// subtitles stay in an isolated staging folder for user review. No existing
// subtitles or video are published, renamed, truncated or overwritten.
func StageSync(ctx context.Context, req SyncRequest, run SyncRunner) (result SyncStage, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if run == nil || req.Binary == "" || req.DurationMS <= 0 || req.StageRoot == "" || req.Video == "" || req.InputSRT == "" {
		return result, errors.New("incomplete sync request")
	}
	videoInfo, e := regularFile(req.Video, 0)
	if e != nil {
		return result, fmt.Errorf("reference video: %w", e)
	}
	_, e = regularFile(req.InputSRT, maxSRTBytes)
	if e != nil {
		return result, fmt.Errorf("source SRT: %w", e)
	}
	stageInfo, e := os.Lstat(req.StageRoot)
	if e != nil {
		return result, e
	}
	if !stageInfo.IsDir() || stageInfo.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("staging root must be a regular directory")
	}
	if e = stageOutsideMedia(req.Video, req.StageRoot); e != nil {
		return result, e
	}
	input, e := os.ReadFile(req.InputSRT)
	if e != nil {
		return result, e
	}
	if _, e = AuditSRT(input, 0); e != nil {
		return result, fmt.Errorf("source subtitle structure invalid: %w", e)
	}
	sourceSHA := sha256.Sum256(input)
	work, e := os.MkdirTemp(req.StageRoot, "subtitle-sync-")
	if e != nil {
		return result, e
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(work)
		}
	}()
	out := filepath.Join(work, "candidate.en.srt")
	// Explicit output path avoids ffsubsync's adjacent-file auto-detection and
	// never enables --overwrite-input. Low-confidence alignments are rejected.
	args := []string{req.Video, "-i", req.InputSRT, "-o", out,
		"--skip-sync-on-low-quality", "--multi-segment-sync",
		"--segment-count", "8", "--skip-intro-outro", "--strict"}
	// Bound even a caller that supplied a context without a deadline.
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if e = run(runCtx, req.Binary, args...); e != nil {
		return result, e
	}
	if e = runCtx.Err(); e != nil {
		return result, e
	}
	// Reject tools that modified inputs or silently raced the reference.
	now, e := os.Stat(req.Video)
	if e != nil {
		return result, e
	}
	if videoInfo.Size() != now.Size() || !videoInfo.ModTime().Equal(now.ModTime()) {
		return result, errors.New("reference media changed during staging")
	}
	sourceAfter, e := os.ReadFile(req.InputSRT)
	if e != nil {
		return result, e
	}
	if sha256.Sum256(sourceAfter) != sourceSHA {
		return result, errors.New("input subtitles were modified during staging")
	}
	if _, e = regularFile(out, maxSRTBytes); e != nil {
		return result, fmt.Errorf("synchronizer produced invalid output: %w", e)
	}
	payload, e := os.ReadFile(out)
	if e != nil {
		return result, e
	}
	report, e := AuditSRT(payload, req.DurationMS)
	if e != nil {
		return result, fmt.Errorf("staged subtitle audit rejected: %w", e)
	}
	result = SyncStage{OutputSRT: out, InputSHA256: hex.EncodeToString(sourceSHA[:]),
		OutputSHA256: report.SHA256, Audit: report,
		ReadyForReview: report.Status == "ready" && report.SHA256 != hex.EncodeToString(sourceSHA[:])}
	success = true
	return result, nil
}
