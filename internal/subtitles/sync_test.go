package subtitles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syncFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, "media")
	staging := filepath.Join(dir, "staging")
	for _, p := range []string{mediaDir, staging} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(mediaDir, "Film (2026).mkv")
	input := filepath.Join(mediaDir, "untrusted.en.srt")
	if err := os.WriteFile(video, []byte("test-video"), 0600); err != nil {
		t.Fatal(err)
	}
	subtitle := []byte("1\n00:00:02,000 --> 00:00:03,000\nHello, world!\n")
	if err := os.WriteFile(input, subtitle, 0600); err != nil {
		t.Fatal(err)
	}
	return video, input, staging
}

func TestSyncStageUsesStrictExplicitOutputAndNeverModifiesSources(t *testing.T) {
	video, input, staging := syncFixture(t)
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	originalVideo, _ := os.ReadFile(video)
	var invoked bool
	run := func(ctx context.Context, binary string, args ...string) error {
		invoked = true
		if binary != "fake-ffs" {
			t.Fatalf("binary %s", binary)
		}
		joined := strings.Join(args, " ")
		for _, required := range []string{
			"--skip-sync-on-low-quality", "--multi-segment-sync", "--skip-intro-outro",
			"-i " + input, "-o ",
		} {
			if !strings.Contains(joined, required) {
				t.Fatalf("missing flag %q in %s", required, joined)
			}
		}
		if strings.Contains(joined, "--overwrite-input") {
			t.Fatal("sync cannot overwrite inputs")
		}
		if args[0] != video {
			t.Fatalf("wrong reference %s", args[0])
		}
		dest := ""
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-o" {
				dest = args[i+1]
			}
		}
		if dest == "" || !strings.HasPrefix(dest, staging+string(os.PathSeparator)) {
			t.Fatalf("bad output %s", dest)
		}
		return os.WriteFile(dest, []byte("1\n00:00:02,100 --> 00:00:03,100\nHello, world!\n"), 0600)
	}
	staged, err := StageSync(context.Background(), SyncRequest{
		Video: video, InputSRT: input, StageRoot: staging, Binary: "fake-ffs", DurationMS: 6000,
	}, run)
	if err != nil {
		t.Fatal(err)
	}
	if !invoked || !staged.ReadyForReview || staged.Audit.Cues != 1 || staged.OutputSHA256 == "" || staged.InputSHA256 == "" {
		t.Fatalf("wrong result %+v", staged)
	}
	if staged.OutputSHA256 == staged.InputSHA256 {
		t.Fatalf("result unchanged unexpectedly")
	}
	if data, _ := os.ReadFile(video); !bytes.Equal(data, originalVideo) {
		t.Fatal("video changed")
	}
	if data, _ := os.ReadFile(input); !bytes.Equal(data, before) {
		t.Fatal("source subtitles changed")
	}
	if data, err := os.ReadFile(staged.OutputSRT); err != nil || !bytes.Contains(data, []byte("02,100")) {
		t.Fatalf("stage missing %v", err)
	}
	if _, err := os.Stat(strings.TrimSuffix(video, filepath.Ext(video)) + ".en.srt"); !os.IsNotExist(err) {
		t.Fatalf("published sidecar without approval: %v", err)
	}
}

func TestSyncStageFailuresLeaveNoArtifacts(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		actor func(output string) error
	}{
		{"tool-exit", func(output string) error { return errors.New("external failure") }},
		{"empty-output", func(output string) error { return os.WriteFile(output, nil, 0600) }},
		{"bad-output", func(output string) error { return os.WriteFile(output, []byte("not subtitles"), 0600) }},
		{"excess-output", func(output string) error { return os.WriteFile(output, bytes.Repeat([]byte{'a'}, 3<<20), 0600) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			video, input, staging := syncFixture(t)
			fn := func(ctx context.Context, binary string, args ...string) error {
				output := ""
				for i := 0; i < len(args)-1; i++ {
					if args[i] == "-o" {
						output = args[i+1]
					}
				}
				return scenario.actor(output)
			}
			_, err := StageSync(context.Background(), SyncRequest{Video: video, InputSRT: input, StageRoot: staging, Binary: "fake-ffs", DurationMS: 6000}, fn)
			if err == nil {
				t.Fatal("expected refusal")
			}
			entries, e := os.ReadDir(staging)
			if e != nil {
				t.Fatal(e)
			}
			if len(entries) != 0 {
				t.Fatalf("failure leaked staged data: %v", entries)
			}
		})
	}
}

func TestSyncStageRejectsMediaDirectoryStageAndCancelledRun(t *testing.T) {
	video, input, staging := syncFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	fn := func(context.Context, string, ...string) error { called = true; return nil }
	_, err := StageSync(ctx, SyncRequest{Video: video, InputSRT: input, StageRoot: staging, Binary: "fake-ffs", DurationMS: 6000}, fn)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancellation not honored: %v called %v", err, called)
	}
	_, err = StageSync(context.Background(), SyncRequest{Video: video, InputSRT: input, StageRoot: filepath.Dir(video), Binary: "fake-ffs", DurationMS: 6000}, fn)
	if err == nil || called {
		t.Fatalf("media directory used for staging: %v called %v", err, called)
	}
}

func TestSyncStageUnchangedAlignmentIsHeldForReview(t *testing.T) {
	video, input, root := syncFixture(t)
	src, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	r, err := StageSync(context.Background(), SyncRequest{Video: video, InputSRT: input,
		StageRoot: root, Binary: "fake-ffs", DurationMS: 6000},
		func(ctx context.Context, binary string, args ...string) error {
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-o" {
					return os.WriteFile(args[i+1], src, 0600)
				}
			}
			return errors.New("missing output flag")
		})
	if err != nil {
		t.Fatal(err)
	}
	if r.ReadyForReview {
		t.Fatalf("unchanged output must not be auto approved: %+v", r)
	}
	if r.Audit.Status != "ready" {
		t.Fatalf("expected structural validity, got %s", r.Audit.Status)
	}
}

func TestSyncStageRejectsSourceMutationAndRemovesTemp(t *testing.T) {
	video, input, root := syncFixture(t)
	_, err := StageSync(context.Background(), SyncRequest{Video: video, InputSRT: input,
		StageRoot: root, Binary: "fake-ffs", DurationMS: 6000},
		func(ctx context.Context, binary string, args ...string) error {
			if err := os.WriteFile(input, []byte("modified"), 0600); err != nil {
				return err
			}
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-o" {
					return os.WriteFile(args[i+1], []byte("1\n00:00:01,000 --> 00:00:02,000\nyes\n"), 0600)
				}
			}
			return errors.New("missing output")
		})
	if err == nil || !strings.Contains(err.Error(), "input subtitles were modified") {
		t.Fatalf("failed to catch source mutation: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed stage cleanup: %v", entries)
	}
}

func TestExecFFsubsyncRequiresTrustedAbsoluteExecutable(t *testing.T) {
	err := ExecFFsubsync(context.Background(), "ffs", "--version")
	if err == nil {
		t.Fatal("PATH-resolved untrusted executable accepted")
	}
}
