package csfs

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"time"
)

type sshServerConn struct {
	Username []byte
	Port     int64
}

type sshServer struct {
	codespace string

	ghProcess *exec.Cmd
	ready     chan sshServerConn
}

func newSSHServer(codespace string) *sshServer {
	return &sshServer{
		codespace: codespace,
		ready:     make(chan sshServerConn),
	}
}

func (s *sshServer) Close() error {
	if s.ghProcess != nil {
		return s.ghProcess.Cancel()
	}
	return nil
}

func (s *sshServer) Listen(ctx context.Context) error {
	errch := make(chan error, 3)       // writer + process + ensureReady
	wch := make(chan sshServerConn, 1) // writer
	w := newWriter(errch, wch)
	args := []string{"cs", "ssh", "-c", s.codespace, "--server-port=0", "--", "-tt"}
	s.ghProcess = exec.CommandContext(ctx, "gh", args...)
	s.ghProcess.Stderr = w
	s.ghProcess.Stdout = w
	go func() {
		select {
		case <-ctx.Done():
			return
		case conn := <-wch:
			// writer has received the connection details, test the port is listening
			if err := s.ensureReady(ctx, conn); err != nil {
				errch <- fmt.Errorf("failed to ensure port is ready: %w", err)
				return
			}
			s.ready <- conn
		}
	}()
	go func() {
		errch <- s.ghProcess.Run()
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errch:
		return err
	}
}

func (s *sshServer) ensureReady(ctx context.Context, c sshServerConn) error {
	dialer := net.Dialer{
		Timeout: 500 * time.Millisecond,
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf(":%d", c.Port))
			if err == nil {
				conn.Close()
				return nil
			}
		}
	}
	return nil
}

func (s *sshServer) Ready() <-chan sshServerConn {
	return s.ready
}

type writer struct {
	errch chan error
	ready chan sshServerConn
}

func newWriter(errch chan error, ready chan sshServerConn) *writer {
	return &writer{
		errch: errch,
		ready: ready,
	}
}

func (w *writer) Write(p []byte) (n int, err error) {
	if bytes.HasPrefix(p, []byte("Connection Details")) {
		p := bytes.Split(p, []byte(" "))
		// Format is: Connection Details: ssh codespace@localhost [-p 1234 ...]
		// There should be at least 6 parts
		if len(p) < 6 {
			w.errch <- fmt.Errorf("invalid connection details: %s", p)
			return len(p), nil
		}
		// The username is in the 4th part
		uhost := bytes.Split(p[3], []byte("@"))
		if len(uhost) != 2 {
			w.errch <- fmt.Errorf("invalid connection details for username: %s", p)
			return len(p), nil
		}
		username := uhost[0]
		// The port is in the 6th part
		port, err := strconv.ParseInt(string(p[5]), 10, 0)
		if err != nil {
			w.errch <- fmt.Errorf("invalid connection details for port: %s", p)
			return len(p), nil
		}
		w.ready <- sshServerConn{
			Username: username,
			Port:     port,
		}
		close(w.ready)
	}
	return len(p), nil
}
