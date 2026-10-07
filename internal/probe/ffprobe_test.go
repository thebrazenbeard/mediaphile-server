package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFFProbeJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "probe", "movie.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseFFProbeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "matroska" || info.DurationMS != 120500 || info.Bitrate != 5000000 {
		t.Fatalf("format normalization mismatch: %#v", info)
	}
	if len(info.Streams) != 3 {
		t.Fatalf("streams=%d want=3", len(info.Streams))
	}
	if info.Streams[0].Kind != StreamVideo || info.Streams[0].Codec != "h264" || info.Streams[0].Width != 1920 {
		t.Fatalf("video stream mismatch: %#v", info.Streams[0])
	}
	if info.Streams[1].Kind != StreamAudio || info.Streams[1].Channels != 6 || info.Streams[1].Language != "eng" {
		t.Fatalf("audio stream mismatch: %#v", info.Streams[1])
	}
	if info.Streams[2].Kind != StreamSubtitle || !info.Streams[2].Forced {
		t.Fatalf("subtitle stream mismatch: %#v", info.Streams[2])
	}
}

func TestParseFFProbeDetectsHDR(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "probe", "episode.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseFFProbeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HDR || info.VideoCodec != "hevc" || info.AudioCodec != "eac3" {
		t.Fatalf("codec/HDR mismatch: %#v", info)
	}
}
