package csfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AlecAivazis/survey/v2"
)

type App struct{}

func NewApp() *App {
	return &App{}
}

func (a *App) Run(ctx context.Context, codespace, workspace string) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if codespace == "" {
		codespace, err = a.pickCodespace(ctx)
		if err != nil {
			return fmt.Errorf("pick codespace failed: %w", err)
		}
	}
	if workspace == "" {
		workspace = "codespace"
	}

	// Start the SSH Server and wait for it to be ready,
	// timeout after 10 seconds, or if the server fails to start.
	sshServerPort := 1234 // TODO(josebalius): Pick a random port.
	sshServer := newSSHServer(sshServerPort, codespace)
	defer func() {
		if closeErr := sshServer.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("ssh server close failed: %w", closeErr)
		}
	}()

	errch := make(chan error, 2) // sshServer, watcher
	defer close(errch)

	fmt.Println("Connecting to Codespace...")
	go func() {
		if err := sshServer.Listen(ctx); err != nil {
			errch <- fmt.Errorf("ssh server failed: %w", err)
		}
	}()

	// Wait for the ssh server to be ready, or timeout.
	fmt.Println("Waiting for server to be ready...")
	sshServerCtx, sshServerCancel := context.WithTimeout(ctx, 10*time.Second)
	defer sshServerCancel()
	if err := a.waitForSSHServer(sshServerCtx, errch, sshServer); err != nil {
		if err == context.DeadlineExceeded {
			return errors.New("SSH Server timed out")
		}
		return fmt.Errorf("ssh server ready failed: %w", err)
	}

	fmt.Println("Server is ready.")
	codespaceDir := fmt.Sprintf("codespace@localhost:/workspaces/%s", workspace)
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd failed: %w", err)
	}
	localDir := filepath.Join(wd, workspace)
	excludes := []string{".git"}
	syncer := newSyncer(sshServerPort, localDir, codespaceDir, excludes)

	// Sync the workspace dir to the current directory. This sync omits
	// the .git directory.
	fmt.Println("Syncing codespace to local...")
	if err := syncer.SyncToLocal(ctx); err != nil {
		return fmt.Errorf("sync to local failed: %w", err)
	}

	// Start the file watcher and rsync on debounced changes, half a second.
	watcher, err := newWatcher(syncer)
	if err != nil {
		return fmt.Errorf("new watcher failed: %w", err)
	}
	fmt.Println("Starting watcher...")
	go func() {
		if err := watcher.Watch(ctx); err != nil {
			errch <- fmt.Errorf("watcher failed: %w", err)
		}
	}()

	fmt.Println("Available commands: [s = sync from codespace to local, q = quit]")

	// Wait for the watcher or the ssh server to fail.
	select {
	case err := <-errch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (a *App) pickCodespace(ctx context.Context) (string, error) {
	codespaces, err := ListCodespaces(ctx)
	if err != nil {
		return "", fmt.Errorf("list codespaces failed: %w", err)
	}
	var codespacesByName []string
	codespacesIndex := make(map[string]Codespace)
	for _, codespace := range codespaces {
		name := fmt.Sprintf("%s: %s", codespace.Repository.FullName, codespace.DisplayName)
		codespacesByName = append(codespacesByName, name)
		codespacesIndex[name] = codespace
	}

	qs := []*survey.Question{
		{
			Name: "codespace",
			Prompt: &survey.Select{
				Message: "Choose codespace:",
				Options: codespacesByName,
			},
		},
	}
	answers := struct {
		Codespace string
	}{}
	if err := survey.Ask(qs, &answers); err != nil {
		return "", fmt.Errorf("survey failed: %w", err)
	}
	codespace, ok := codespacesIndex[answers.Codespace]
	if !ok {
		return "", fmt.Errorf("codespace not found: %s", answers.Codespace)
	}
	return codespace.Name, nil
}

func (a *App) waitForSSHServer(ctx context.Context, errch chan error, s *sshServer) error {
	select {
	case err := <-errch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.Ready():
	}
	return nil
}
