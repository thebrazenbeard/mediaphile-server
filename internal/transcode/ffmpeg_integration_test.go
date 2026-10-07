//go:build ffmpeg

package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

func TestRealFFmpegRemuxProducesHLS(t *testing.T) {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	root := t.TempDir()
	input := filepath.Join(root, "input.mp4")
	makeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	makeCmd := exec.CommandContext(makeCtx, binary,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=160x90:d=1",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=1",
		"-c:v", "libx264", "-c:a", "aac", "-shortest", input)
	if out, err := makeCmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture ffmpeg: %v: %s", err, out)
	}
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	args, err := BuildArgs(Request{InputPath: input, OutputDir: outDir, Mode: playback.Remux})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, runCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer runCancel()
	cmd := exec.CommandContext(runCtx, binary, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("remux ffmpeg: %v: %s", err, out)
	}
	if info, err := os.Stat(filepath.Join(outDir, "master.m3u8")); err != nil || info.Size() == 0 {
		t.Fatalf("playlist missing/empty: info=%v err=%v", info, err)
	}
}
