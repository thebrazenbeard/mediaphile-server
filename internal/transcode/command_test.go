package transcode

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

func TestBuildArgsKeepsHostileFilenameSingleArgument(t *testing.T) {
	input := `C:\media\movie; echo pwned & x.mkv`
	out := filepath.Join(t.TempDir(), "session")
	args, err := BuildArgs(Request{InputPath: input, OutputDir: out, Mode: playback.Transcode, Reasons: []string{"VIDEO_CODEC_UNSUPPORTED"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, v := range args {
		if v == "-i" && i+1 < len(args) && args[i+1] == input {
			found = true
		}
	}
	if !found {
		t.Fatalf("input path not preserved as one argv entry: %#v", args)
	}
}

func TestBuildArgsRemuxCopiesCodecs(t *testing.T) {
	args, err := BuildArgs(Request{InputPath: "movie.mkv", OutputDir: t.TempDir(), Mode: playback.Remux})
	if err != nil {
		t.Fatal(err)
	}
	if !hasPair(args, "-c:v", "copy") || !hasPair(args, "-c:a", "copy") {
		t.Fatalf("args=%v", args)
	}
}

func TestBuildArgsTranscodesOnlyIncompatibleStream(t *testing.T) {
	args, err := BuildArgs(Request{InputPath: "movie.mkv", OutputDir: t.TempDir(), Mode: playback.Transcode, Reasons: []string{"VIDEO_CODEC_UNSUPPORTED"}})
	if err != nil {
		t.Fatal(err)
	}
	if !hasPair(args, "-c:v", "libx264") || !hasPair(args, "-c:a", "copy") {
		t.Fatalf("args=%v", args)
	}
}

func TestBuildArgsSubtitleBurnAddsFilter(t *testing.T) {
	args, err := BuildArgs(Request{InputPath: "movie.mkv", OutputDir: t.TempDir(), Mode: playback.Transcode, Reasons: []string{"SUBTITLE_BURN_REQUIRED"}, SubtitleStreamIndex: ptrInt(2)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, v := range args {
		if v == "-vf" && i+1 < len(args) && strings.Contains(args[i+1], "subtitles=") {
			found = true
		}
	}
	if !found {
		t.Fatalf("subtitle filter absent: %v", args)
	}
}

func hasPair(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}
func ptrInt(v int) *int { return &v }
