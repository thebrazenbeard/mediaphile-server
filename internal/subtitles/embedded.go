package subtitles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type EmbeddedTrack struct {
	Index    int    `json:"index"`
	Codec    string `json:"codec"`
	Language string `json:"language"`
}
type EmbeddedAudit struct {
	Index  int    `json:"index"`
	Codec  string `json:"codec"`
	Status string `json:"status"`
	Audit  *Audit `json:"audit,omitempty"`
	Reason string `json:"reason,omitempty"`
}
type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxSRTBytes {
		return 0, errors.New("embedded subtitle extraction exceeds size limit")
	}
	return b.Buffer.Write(data)
}
func EnglishTrack(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "en", "eng", "english":
		return true
	default:
		return false
	}
}
func InspectEmbedded(ctx context.Context, ffmpeg, video string, streams []EmbeddedTrack, durationMS int64) []EmbeddedAudit {
	return inspectEmbedded(ctx, ffmpeg, video, nil, streams, durationMS)
}

// InspectEmbeddedInLibrary never gives FFmpeg the catalog path. It opens the
// media through the configured library root, pins the validated regular file,
// and provides that handle as FFmpeg's standard input. Demuxers that require
// seeking a pathname may fail; this is an unverified result, never a reason to
// retry via an unconfined path.
func InspectEmbeddedInLibrary(ctx context.Context, ffmpeg, libraryRoot, video string, size, modTimeNS int64, streams []EmbeddedTrack, durationMS int64) []EmbeddedAudit {
	if ffmpeg == "" {
		return InspectEmbedded(ctx, "", "", streams, durationMS)
	}
	file, err := openEmbeddedMediaInLibrary(libraryRoot, video, size, modTimeNS)
	if err != nil {
		out := []EmbeddedAudit{}
		for _, s := range streams {
			if EnglishTrack(s.Language) {
				out = append(out, EmbeddedAudit{Index: s.Index, Codec: s.Codec, Status: "unverified", Reason: "cataloged media is not safely accessible inside the library"})
			}
		}
		return out
	}
	defer file.Close()
	return inspectEmbedded(ctx, ffmpeg, "pipe:0", file, streams, durationMS)
}

func openEmbeddedMediaInLibrary(libraryRoot, video string, size, modTimeNS int64) (*os.File, error) {
	mediaRoot, err := openLibraryMediaRoot(libraryRoot, video, size, modTimeNS)
	if err != nil {
		return nil, err
	}
	defer mediaRoot.Close()
	name := filepath.Base(video)
	before, err := mediaRoot.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != size || before.ModTime().UnixNano() != modTimeNS {
		return nil, errors.New("cataloged media changed before embedded inspection")
	}
	file, err := mediaRoot.Open(name)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(before, actual) {
		file.Close()
		return nil, errors.New("cataloged media changed during embedded inspection opening")
	}
	return file, nil
}

func inspectEmbedded(ctx context.Context, ffmpeg, video string, input *os.File, streams []EmbeddedTrack, durationMS int64) []EmbeddedAudit {
	out := []EmbeddedAudit{}
	for _, s := range streams {
		if !EnglishTrack(s.Language) {
			continue
		}
		result := EmbeddedAudit{Index: s.Index, Codec: s.Codec, Status: "unverified"}
		if s.Index < 0 || s.Index > 255 {
			result.Reason = "invalid stream index"
			out = append(out, result)
			continue
		}
		switch strings.ToLower(s.Codec) {
		case "mov_text", "subrip", "ass", "ssa", "webvtt":
		default:
			result.Reason = "image-based or unsupported embedded subtitle codec; needs separate OCR"
			out = append(out, result)
			continue
		}
		if ffmpeg == "" {
			result.Reason = "ffmpeg not configured"
			out = append(out, result)
			continue
		}
		if input != nil {
			if _, err := input.Seek(0, io.SeekStart); err != nil {
				result.Reason = "pinned media input cannot be rewound"
				out = append(out, result)
				continue
			}
		}
		processCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		command := exec.CommandContext(processCtx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-i", video, "-map", "0:"+strconv.Itoa(s.Index), "-c:s", "srt", "-f", "srt", "pipe:1")
		if input != nil {
			command.Stdin = input
		}
		var buffer cappedBuffer
		command.Stdout = &buffer
		var stderr bytes.Buffer
		command.Stderr = &stderr
		err := command.Run()
		cancel()
		if err != nil {
			result.Reason = fmt.Sprintf("subtitle extraction failed: %v", err)
			out = append(out, result)
			continue
		}
		audit, err := AuditSRT(buffer.Bytes(), durationMS)
		if err != nil {
			result.Reason = "extracted subtitles failed SRT validation"
			out = append(out, result)
			continue
		}
		result.Audit = &audit
		result.Status = audit.Status
		out = append(out, result)
	}
	return out
}
