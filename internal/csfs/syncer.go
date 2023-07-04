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
	case syncTypeLocalWithDeletion:
		return "local w/ deletion"
	default:
		return "unknown"
	}
}

const (
	syncTypeCodespace syncType = iota
	syncTypeLocal
	syncTypeLocalWithDeletion
)

type syncer struct {
	port         int64
	localDir     string
	codespaceDir string
	excludes     []string
	debounce     time.Duration

	syncToCodespace chan struct{}
	syncEvent       chan syncType
}

func newSyncer(port int64, localDir, codespaceDir string, excludes []string, debounce time.Duration) *syncer {
	return &syncer{
		port:            port,
		localDir:        localDir,
		codespaceDir:    codespaceDir,
		excludes:        excludes,
		debounce:        debounce,
		syncToCodespace: make(chan struct{}),
		syncEvent:       make(chan syncType),
	}
}

func (s *syncer) Event() <-chan syncType {
	return s.syncEvent
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
	ticker := time.NewTicker(s.debounce)
	for {
		select {
		case <-ctx.Done():
			return nil
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
		"--hard-links",
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
	t := syncTypeCodespace
	if dest == s.localDir {
		t = syncTypeLocal
		if deleteFiles {
			t = syncTypeLocalWithDeletion
		}
	}
	select {
	case s.syncEvent <- t:
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
