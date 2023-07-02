package csfs

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type syncer struct {
	port         int
	localDir     string
	codespaceDir string
	excludes     []string

	syncToCodespace chan struct{}
}

func newSyncer(port int, localDir, codespaceDir string, excludes []string) *syncer {
	return &syncer{
		port:            port,
		localDir:        localDir,
		codespaceDir:    codespaceDir,
		excludes:        excludes,
		syncToCodespace: make(chan struct{}),
	}
}

func (s *syncer) SyncToLocal(ctx context.Context) error {
	return s.sync(ctx, s.codespaceDir, s.localDir, s.excludes)
}

func (s *syncer) SyncToCodespace(ctx context.Context) {
	select {
	case s.syncToCodespace <- struct{}{}:
	default:
	}
}

func (s *syncer) Sync(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			<-s.syncToCodespace
			if err := s.sync(ctx, s.localDir, s.codespaceDir, s.excludes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *syncer) sync(ctx context.Context, src, dest string, excludePaths []string) error {
	args := []string{
		"--archive",
		"--compress",
		"--delete",
		"--update",
		"-e",
		fmt.Sprintf("ssh -p %d -o NoHostAuthenticationForLocalhost=yes -o PasswordAuthentication=no", s.port),
	}
	for _, exclude := range excludePaths {
		args = append(args, "--exclude", exclude)
	}
	args = append(args, srcDirWithSuffix(src), dest)
	cmd := exec.CommandContext(ctx, "rsync", args...)
	return cmd.Run()
}

func srcDirWithSuffix(src string) string {
	if src[len(src)-1] != '/' {
		src += "/"
	}
	return src
}
