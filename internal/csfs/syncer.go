package csfs

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type syncType int

func (s syncType) String() string {
	switch s {
	case syncTypeCodespace:
		return "codespace"
	case syncTypeLocal:
		return "local"
	default:
		return "unknown"
	}
}

const (
	syncTypeCodespace syncType = iota
	syncTypeLocal
)

type syncer struct {
	port         int
	localDir     string
	codespaceDir string
	excludes     []string

	syncToCodespace chan struct{}
	syncNotify      chan syncType
}

func newSyncer(port int, localDir, codespaceDir string, excludes []string) *syncer {
	return &syncer{
		port:            port,
		localDir:        localDir,
		codespaceDir:    codespaceDir,
		excludes:        excludes,
		syncToCodespace: make(chan struct{}),
		syncNotify:      make(chan syncType),
	}
}

func (s *syncer) SyncNotify() <-chan syncType {
	return s.syncNotify
}

func (s *syncer) SyncToLocal(ctx context.Context, deleteFiles bool) error {
	return s.sync(ctx, s.codespaceDir, s.localDir, s.excludes, deleteFiles)
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
			if err := s.sync(ctx, s.localDir, s.codespaceDir, s.excludes, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *syncer) sync(ctx context.Context, src, dest string, excludePaths []string, deleteFiles bool) error {
	args := []string{
		"--archive",
		"--compress",
		"--update",
		"--perms",
		"-e",
		fmt.Sprintf("ssh -p %d -o NoHostAuthenticationForLocalhost=yes -o PasswordAuthentication=no", s.port),
	}
	if deleteFiles {
		args = append(args, "--delete")
	}
	for _, exclude := range excludePaths {
		args = append(args, "--exclude", exclude)
	}
	args = append(args, srcDirWithSuffix(src), dest)
	cmd := exec.CommandContext(ctx, "rsync", args...)
	if err := cmd.Run(); err != nil {
		return err
	}

	t := syncTypeLocal
	if src == s.localDir {
		t = syncTypeCodespace
	}
	select {
	case s.syncNotify <- t:
	default:
	}

	return nil
}

func srcDirWithSuffix(src string) string {
	if src[len(src)-1] != '/' {
		src += "/"
	}
	return src
}
