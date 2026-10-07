package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type StreamKind string

const (
	StreamVideo    StreamKind = "video"
	StreamAudio    StreamKind = "audio"
	StreamSubtitle StreamKind = "subtitle"
)

type Stream struct {
	Index     int
	Kind      StreamKind
	Codec     string
	Language  string
	Channels  int
	Width     int
	Height    int
	FrameRate string
	Default   bool
	Forced    bool
}

type MediaInfo struct {
	Container  string
	DurationMS int64
	Bitrate    int64
	Width      int
	Height     int
	VideoCodec string
	AudioCodec string
	HDR        bool
	Streams    []Stream
}

type Prober interface {
	Probe(context.Context, string) (MediaInfo, error)
}

type FFProbe struct {
	Binary string
}

func NewFFProbe(binary string) FFProbe {
	if strings.TrimSpace(binary) == "" {
		binary = "ffprobe"
	}
	return FFProbe{Binary: binary}
}

func (p FFProbe) Probe(ctx context.Context, path string) (MediaInfo, error) {
	cmd := exec.CommandContext(ctx, p.Binary, "-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe %q: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	info, err := ParseFFProbeJSON(out)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("parse ffprobe %q: %w", path, err)
	}
	return info, nil
}

type rawProbe struct {
	Streams []struct {
		Index         int    `json:"index"`
		CodecName     string `json:"codec_name"`
		CodecType     string `json:"codec_type"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		Channels      int    `json:"channels"`
		RFrameRate    string `json:"r_frame_rate"`
		ColorTransfer string `json:"color_transfer"`
		Tags          struct {
			Language string `json:"language"`
		} `json:"tags"`
		Disposition struct {
			Default int `json:"default"`
			Forced  int `json:"forced"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

func ParseFFProbeJSON(data []byte) (MediaInfo, error) {
	var raw rawProbe
	if err := json.Unmarshal(data, &raw); err != nil {
		return MediaInfo{}, err
	}
	var out MediaInfo
	if raw.Format.FormatName != "" {
		out.Container = strings.Split(raw.Format.FormatName, ",")[0]
	}
	if raw.Format.Duration != "" {
		seconds, err := strconv.ParseFloat(raw.Format.Duration, 64)
		if err != nil {
			return MediaInfo{}, fmt.Errorf("duration %q: %w", raw.Format.Duration, err)
		}
		out.DurationMS = int64(seconds*1000 + 0.5)
	}
	if raw.Format.BitRate != "" {
		bitrate, err := strconv.ParseInt(raw.Format.BitRate, 10, 64)
		if err != nil {
			return MediaInfo{}, fmt.Errorf("bit rate %q: %w", raw.Format.BitRate, err)
		}
		out.Bitrate = bitrate
	}
	for _, s := range raw.Streams {
		var kind StreamKind
		switch s.CodecType {
		case "video":
			kind = StreamVideo
		case "audio":
			kind = StreamAudio
		case "subtitle":
			kind = StreamSubtitle
		default:
			continue
		}
		stream := Stream{
			Index: s.Index, Kind: kind, Codec: s.CodecName, Language: s.Tags.Language,
			Channels: s.Channels, Width: s.Width, Height: s.Height, FrameRate: s.RFrameRate,
			Default: s.Disposition.Default != 0, Forced: s.Disposition.Forced != 0,
		}
		out.Streams = append(out.Streams, stream)
		if kind == StreamVideo && out.VideoCodec == "" {
			out.VideoCodec = s.CodecName
			out.Width, out.Height = s.Width, s.Height
			switch strings.ToLower(s.ColorTransfer) {
			case "smpte2084", "arib-std-b67":
				out.HDR = true
			}
		}
		if kind == StreamAudio && out.AudioCodec == "" {
			out.AudioCodec = s.CodecName
		}
	}
	return out, nil
}
