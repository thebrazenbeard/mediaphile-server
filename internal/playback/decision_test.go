package playback

import "testing"

func baseCaps() Capabilities {
	return Capabilities{
		ClientID: "browser", Containers: []string{"mp4"}, VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"}, SubtitleCodecs: []string{"webvtt"},
		MaxWidth: 3840, MaxHeight: 2160, MaxVideoBitrate: 40_000_000, HLS: true, RangeRequests: true,
	}
}

func TestDecisionModes(t *testing.T) {
	tests := []struct {
		name   string
		source Source
		caps   Capabilities
		ffmpeg bool
		want   Mode
		reason string
	}{
		{"direct", Source{ID: "s", PartID: "p", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, Bitrate: 8_000_000, Available: true}, baseCaps(), true, DirectPlay, ""},
		{"remux", Source{ID: "s", PartID: "p", Container: "mkv", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, Bitrate: 8_000_000, Available: true}, baseCaps(), true, Remux, "CONTAINER_UNSUPPORTED"},
		{"video transcode", Source{ID: "s", PartID: "p", Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac", Width: 1920, Height: 1080, Bitrate: 8_000_000, Available: true}, baseCaps(), true, Transcode, "VIDEO_CODEC_UNSUPPORTED"},
		{"quality transcode", Source{ID: "s", PartID: "p", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 3840, Height: 2160, Bitrate: 60_000_000, Available: true}, baseCaps(), true, Transcode, "BITRATE_LIMIT"},
		{"no transform", Source{ID: "s", PartID: "p", Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac", Width: 1920, Height: 1080, Bitrate: 8_000_000, Available: true}, baseCaps(), false, Unplayable, "NO_TRANSFORM_CAPABILITY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide([]Source{tt.source}, tt.caps, Selection{}, tt.ffmpeg)
			if got.Mode != tt.want {
				t.Fatalf("mode=%s want=%s reasons=%v", got.Mode, tt.want, got.Reasons)
			}
			if tt.reason != "" && !containsReason(got.Reasons, tt.reason) {
				t.Fatalf("missing reason %s in %v", tt.reason, got.Reasons)
			}
		})
	}
}

func TestSubtitleBurnRequiresTranscode(t *testing.T) {
	src := Source{ID: "s", PartID: "p", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, Bitrate: 8_000_000, Available: true,
		Streams: []Stream{{ID: "sub", Kind: "subtitle", Codec: "ass"}}}
	got := Decide([]Source{src}, baseCaps(), Selection{SubtitleStreamID: "sub"}, true)
	if got.Mode != Transcode || !containsReason(got.Reasons, "SUBTITLE_BURN_REQUIRED") {
		t.Fatalf("got=%#v", got)
	}
}

func TestDecisionPrefersNoTransformAndIsStable(t *testing.T) {
	sources := []Source{
		{ID: "z-transcode", PartID: "z", Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac", Width: 3840, Height: 2160, Bitrate: 20_000_000, Available: true},
		{ID: "b-direct", PartID: "b", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720, Bitrate: 4_000_000, Available: true},
		{ID: "a-direct", PartID: "a", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720, Bitrate: 4_000_000, Available: true},
	}
	got := Decide(sources, baseCaps(), Selection{}, true)
	if got.Mode != DirectPlay || got.SourceID != "a-direct" {
		t.Fatalf("got=%#v", got)
	}
}

func TestUnavailableSourcesAreExcluded(t *testing.T) {
	got := Decide([]Source{{ID: "s", Available: false}}, baseCaps(), Selection{}, true)
	if got.Mode != Unplayable || !containsReason(got.Reasons, "NO_ACCESSIBLE_SOURCE") {
		t.Fatalf("got=%#v", got)
	}
}

func containsReason(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
