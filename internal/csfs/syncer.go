package csfs

import (
	"context"
	"fmt"
	"os/exec"
)

type syncer struct {
	port         int
	localDir     string
	codespaceDir string
	excludes     []string
}

func newSyncer(port int, localDir, codespaceDir string, excludes []string) *syncer {
	return &syncer{
		port:         port,
		localDir:     localDir,
		codespaceDir: codespaceDir,
		excludes:     excludes,
	}
}

func (s *syncer) SyncToLocal(ctx context.Context) error {
	return s.sync(ctx, s.codespaceDir, s.localDir, s.excludes)
}

func (s *syncer) sync(ctx context.Context, src, dest string, excludePaths []string) error {
	args := []string{
		"--archive",
		"--delete",
		"--human-readable",
		"--verbose",
		"--update",
		"-e",
		fmt.Sprintf(
			"ssh codespace@localhost -p %d -o NoHostAuthenticationForLocalhost=yes -o PasswordAuthentication=no",
			s.port,
		),
	}
	for _, exclude := range excludePaths {
		args = append(args, "--exclude", exclude)
	}
	args = append(args, srcDirWithSuffix(src), dest)
	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmd.Dir = src
	return cmd.Run()
}

func srcDirWithSuffix(src string) string {
	if src[len(src)-1] != '/' {
		src += "/"
	}
	return src
}
