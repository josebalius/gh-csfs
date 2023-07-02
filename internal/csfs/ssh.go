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
	ready     chan string
}

func newSSHServer(port int, codespace string) *sshServer {
	return &sshServer{
		port:      port,
		codespace: codespace,
		ready:     make(chan string),
	}
}

func (s *sshServer) Close() error {
	if s.ghProcess != nil && s.ghProcess.Process != nil {
		return s.ghProcess.Process.Kill()
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
		"--",
		"-tt",
	}

	s.ghProcess = exec.CommandContext(ctx, "gh", args...)
	s.ghProcess.Stderr = w
	s.ghProcess.Stdout = w

	return s.ghProcess.Run()
}

func (s *sshServer) Ready() <-chan string {
	return s.ready
}

type writer struct {
	ready chan string
}

func newWriter(ready chan string) *writer {
	return &writer{
		ready: ready,
	}
}

func (w *writer) Write(p []byte) (n int, err error) {
	if bytes.HasPrefix(p, []byte("Connection Details")) {
		p := bytes.Split(p, []byte("@"))
		if len(p) != 2 {
			return 0, fmt.Errorf("invalid connection details: %s", p)
		}
		p2 := bytes.Split(p[0], []byte(" "))
		if len(p2) != 4 {
			return 0, fmt.Errorf("invalid connection details for username: %s", p)
		}
		w.ready <- string(p2[len(p2)-1])
		close(w.ready)
	}
	return len(p), nil
}
