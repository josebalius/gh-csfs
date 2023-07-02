package csfs

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type sshServer struct {
	port      int
	codespace string

	ghProcess *exec.Cmd
	ready     chan struct{}
}

func newSSHServer(port int, codespace string) *sshServer {
	return &sshServer{
		port:      port,
		codespace: codespace,
		ready:     make(chan struct{}),
	}
}

func (s *sshServer) Close() error {
	if s.ghProcess != nil && s.ghProcess.Process != nil {
		s.ghProcess.Process.Kill()
	}
	return nil
}

func (s *sshServer) Listen(ctx context.Context) error {
	w := newWriter(s.ready)
	args := []string{
		"cs",
		"ssh",
		"-c",
		s.codespace,
		fmt.Sprintf("--server-port=%d", s.port),
	}

	s.ghProcess = exec.CommandContext(ctx, "gh", args...)
	s.ghProcess.Stderr = w
	s.ghProcess.Stdout = w

	if err := s.ghProcess.Start(); err != nil {
		return fmt.Errorf("failed to start gh: %w", err)
	}

	return s.ghProcess.Wait()
}

func (s *sshServer) Ready() <-chan struct{} {
	return s.ready
}

type writer struct {
	ready chan struct{}
}

func newWriter(ready chan struct{}) *writer {
	return &writer{
		ready: ready,
	}
}

func (w *writer) Write(p []byte) (n int, err error) {
	if bytes.HasPrefix(p, []byte("Connection Details")) {
		close(w.ready)
	}
	return len(p), nil
}
