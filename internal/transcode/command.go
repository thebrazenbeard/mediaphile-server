package transcode

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

type Request struct {
	InputPath           string
	OutputDir           string
	Mode                playback.Mode
	Reasons             []string
	SubtitleStreamIndex *int
}

func BuildArgs(req Request) ([]string, error) {
	if req.InputPath == "" {
		return nil, fmt.Errorf("input path is required")
	}
	if req.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	if req.Mode != playback.Remux && req.Mode != playback.Transcode {
		return nil, fmt.Errorf("unsupported transcode mode %q", req.Mode)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", req.InputPath}
	if req.Mode == playback.Remux {
		args = append(args, "-c:v", "copy", "-c:a", "copy", "-c:s", "copy")
	} else {
		videoCodec := "copy"
		if hasAnyReason(req.Reasons, "VIDEO_CODEC_UNSUPPORTED", "RESOLUTION_LIMIT", "BITRATE_LIMIT", "SUBTITLE_BURN_REQUIRED") {
			videoCodec = "libx264"
		}
		audioCodec := "copy"
		if hasAnyReason(req.Reasons, "AUDIO_CODEC_UNSUPPORTED") {
			audioCodec = "aac"
		}
		args = append(args, "-c:v", videoCodec, "-c:a", audioCodec)
		if hasAnyReason(req.Reasons, "SUBTITLE_BURN_REQUIRED") {
			index := 0
			if req.SubtitleStreamIndex != nil {
				index = *req.SubtitleStreamIndex
			}
			filter := fmt.Sprintf("subtitles=filename='%s':si=%s", escapeFilterPath(req.InputPath), strconv.Itoa(index))
			args = append(args, "-vf", filter, "-sn")
		} else {
			args = append(args, "-c:s", "webvtt")
		}
	}
	segmentPattern := filepath.Join(req.OutputDir, "segment-%05d.ts")
	playlist := filepath.Join(req.OutputDir, "master.m3u8")
	args = append(args, "-f", "hls", "-hls_time", "4", "-hls_list_size", "0", "-hls_flags", "independent_segments", "-hls_segment_filename", segmentPattern, playlist)
	return args, nil
}

func hasAnyReason(reasons []string, wants ...string) bool {
	for _, reason := range reasons {
		for _, want := range wants {
			if reason == want {
				return true
			}
		}
	}
	return false
}

func escapeFilterPath(path string) string {
	s := strings.ReplaceAll(path, "\\", "/")
	s = strings.ReplaceAll(s, ":", "\\:")
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
}
