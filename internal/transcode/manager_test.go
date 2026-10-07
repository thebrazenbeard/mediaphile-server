package transcode

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

type fakeProcess struct {
	pid    int
	mu     sync.Mutex
	killed bool
	done   chan struct{}
	once   sync.Once
}

func newFakeProcess(pid int) *fakeProcess { return &fakeProcess{pid: pid, done: make(chan struct{})} }
func (f *fakeProcess) PID() int           { return f.pid }
func (f *fakeProcess) Wait() error        { <-f.done; return nil }
func (f *fakeProcess) Kill() error {
	f.mu.Lock()
	f.killed = true
	f.mu.Unlock()
	f.once.Do(func() { close(f.done) })
	return nil
}
func (f *fakeProcess) wasKilled() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.killed }

type fakeRunner struct {
	proc   *fakeProcess
	binary string
	args   []string
}

func (f *fakeRunner) Start(_ context.Context, binary string, args []string) (Process, error) {
	f.binary = binary
	f.args = append([]string(nil), args...)
	return f.proc, nil
}

func TestManagerConfinesOutputStopsAndExpiresSession(t *testing.T) {
	root := t.TempDir()
	sibling := filepath.Join(root, "keep")
	if err := os.MkdirAll(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{proc: newFakeProcess(42)}
	m := NewManager(root, "ffmpeg", fr)
	info, err := m.Start(context.Background(), "session-1", Request{InputPath: "movie.mkv", Mode: playback.Remux})
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, "session-1")
	if info.OutputDir != wantDir {
		t.Fatalf("output=%q want=%q", info.OutputDir, wantDir)
	}
	if err := m.Stop("session-1"); err != nil {
		t.Fatal(err)
	}
	if !fr.proc.wasKilled() {
		t.Fatal("process not killed")
	}
	if err := m.Expire("session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wantDir); !os.IsNotExist(err) {
		t.Fatalf("session directory still exists: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("sibling removed: %v", err)
	}
}

func TestArtifactPathRejectsTraversal(t *testing.T) {
	m := NewManager(t.TempDir(), "ffmpeg", &fakeRunner{proc: newFakeProcess(1)})
	for _, tc := range []struct{ session, artifact string }{{"../escape", "master.m3u8"}, {"ok", "../secret"}, {"ok", "/absolute"}} {
		if _, err := m.ArtifactPath(tc.session, tc.artifact); err == nil {
			t.Fatalf("expected rejection: %#v", tc)
		}
	}
}
