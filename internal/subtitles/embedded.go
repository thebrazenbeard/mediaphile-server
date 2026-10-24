package subtitles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
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
		processCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		command := exec.CommandContext(processCtx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-i", video, "-map", "0:"+strconv.Itoa(s.Index), "-c:s", "srt", "-f", "srt", "pipe:1")
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
