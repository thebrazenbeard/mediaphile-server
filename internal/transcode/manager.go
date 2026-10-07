package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	ErrInvalidSessionID = errors.New("invalid transcode session id")
	ErrInvalidArtifact  = errors.New("invalid transcode artifact path")
	ErrNotRunning       = errors.New("transcode session is not running")
	sessionPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type Process interface {
	PID() int
	Wait() error
	Kill() error
}
type Runner interface {
	Start(context.Context, string, []string) (Process, error)
}

type execRunner struct{}

func (execRunner) Start(ctx context.Context, binary string, args []string) (Process, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd}, nil
}

type execProcess struct{ cmd *exec.Cmd }

func (p *execProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *execProcess) Wait() error { return p.cmd.Wait() }
func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

type SessionInfo struct {
	ID        string
	PID       int
	OutputDir string
	Playlist  string
}

type running struct {
	proc   Process
	cancel context.CancelFunc
	dir    string
}

type Manager struct {
	root    string
	binary  string
	runner  Runner
	mu      sync.Mutex
	running map[string]running
}

func NewManager(root, binary string, runner Runner) *Manager {
	if binary == "" {
		binary = "ffmpeg"
	}
	if runner == nil {
		runner = execRunner{}
	}
	return &Manager{root: filepath.Clean(root), binary: binary, runner: runner, running: map[string]running{}}
}

func (m *Manager) Start(parent context.Context, sessionID string, req Request) (SessionInfo, error) {
	if !validSessionID(sessionID) {
		return SessionInfo{}, ErrInvalidSessionID
	}
	select {
	case <-parent.Done():
		return SessionInfo{}, parent.Err()
	default:
	}
	dir := filepath.Join(m.root, sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SessionInfo{}, err
	}
	req.OutputDir = dir
	args, err := BuildArgs(req)
	if err != nil {
		return SessionInfo{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := m.runner.Start(ctx, m.binary, args)
	if err != nil {
		cancel()
		return SessionInfo{}, err
	}
	m.mu.Lock()
	if old, ok := m.running[sessionID]; ok {
		m.mu.Unlock()
		cancel()
		_ = proc.Kill()
		_ = old
		return SessionInfo{}, fmt.Errorf("transcode session already running")
	}
	m.running[sessionID] = running{proc: proc, cancel: cancel, dir: dir}
	m.mu.Unlock()
	go func() {
		_ = proc.Wait()
		m.mu.Lock()
		if current, ok := m.running[sessionID]; ok && current.proc == proc {
			delete(m.running, sessionID)
		}
		m.mu.Unlock()
		cancel()
	}()
	return SessionInfo{ID: sessionID, PID: proc.PID(), OutputDir: dir, Playlist: filepath.Join(dir, "master.m3u8")}, nil
}

func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	current, ok := m.running[sessionID]
	if ok {
		delete(m.running, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotRunning
	}
	current.cancel()
	return current.proc.Kill()
}

func (m *Manager) Expire(sessionID string) error {
	if !validSessionID(sessionID) {
		return ErrInvalidSessionID
	}
	if err := m.Stop(sessionID); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	dir := filepath.Join(m.root, sessionID)
	return os.RemoveAll(dir)
}

func (m *Manager) ArtifactPath(sessionID, artifact string) (string, error) {
	if !validSessionID(sessionID) {
		return "", ErrInvalidSessionID
	}
	if artifact == "" || filepath.IsAbs(artifact) || strings.HasPrefix(artifact, "/") || strings.HasPrefix(artifact, "\\") {
		return "", ErrInvalidArtifact
	}
	clean := filepath.Clean(filepath.FromSlash(artifact))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrInvalidArtifact
	}
	base := filepath.Join(m.root, sessionID)
	target := filepath.Join(base, clean)
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidArtifact
	}
	return target, nil
}

func validSessionID(id string) bool { return sessionPattern.MatchString(id) }
