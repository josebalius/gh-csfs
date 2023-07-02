package csfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/briandowns/spinner"
	"github.com/eiannone/keyboard"
)

type App struct {
	spinner *spinner.Spinner
	syncer  *syncer
}

func NewApp() *App {
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
	return &App{
		spinner: s,
	}
}

func (a *App) Run(ctx context.Context, codespace, workspace string, exclude []string, deleteFiles bool) (err error) {
	defer a.opdone()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if codespace == "" {
		c, codespaceWorkspace, err := a.pickCodespace(ctx)
		if err != nil {
			return fmt.Errorf("pick codespace failed: %w", err)
		}
		codespace = c
		if workspace == "" {
			workspace = codespaceWorkspace
		}
	}
	if workspace == "" {
		return errors.New("workspace is required")
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

	errch := make(chan error, 4) // sshServer, watcher, syncer, keyEvents
	defer close(errch)

	a.op("Connecting to codespace")
	go func() {
		if err := sshServer.Listen(ctx); err != nil {
			errch <- fmt.Errorf("ssh server failed: %w", err)
		}
	}()
	a.opdone()

	// Wait for the ssh server to be ready, or timeout.
	a.op("Waiting for server to be ready")
	sshServerCtx, sshServerCancel := context.WithTimeout(ctx, 10*time.Second)
	defer sshServerCancel()
	username, err := a.waitForSSHServer(sshServerCtx, errch, sshServer)
	a.opdone()
	if err != nil {
		if err == context.DeadlineExceeded {
			return errors.New("SSH Server timed out")
		}
		return fmt.Errorf("ssh server ready failed: %w", err)
	}

	a.op("Setting up sync opertions")
	codespaceDir := fmt.Sprintf("%s@localhost:/workspaces/%s", username, workspace)
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd failed: %w", err)
	}
	localDir := filepath.Join(wd, workspace)
	excludes := []string{".git"}
	if len(exclude) > 0 {
		excludes = append(excludes, exclude...)
	}

	a.syncer = newSyncer(sshServerPort, localDir, codespaceDir, excludes)
	syncNotifier := a.syncer.SyncNotify()
	go func() {
		if err := a.syncer.Sync(ctx); err != nil {
			errch <- fmt.Errorf("sync failed: %w", err)
		}
	}()
	a.opdone()

	// Sync the workspace dir to the current directory. This sync omits
	// the .git directory.
	a.op("Syncing codespace to local")
	if err := a.syncer.SyncToLocal(ctx, deleteFiles); err != nil {
		return fmt.Errorf("sync to local failed: %w", err)
	}
	a.opdone()

	// Start the file watcher and rsync on debounced changes, half a second.
	a.op("Starting file watcher")
	watcher, err := newWatcher(a.syncer)
	if err != nil {
		return fmt.Errorf("new watcher failed: %w", err)
	}
	go func() {
		if err := watcher.Watch(ctx); err != nil {
			errch <- fmt.Errorf("watcher failed: %w", err)
		}
	}()
	a.opdone()

	keyEvents, err := keyboard.GetKeys(1)
	if err != nil {
		return fmt.Errorf("get keys failed: %w", err)
	}

	a.showAvailableCommands()
	exit := make(chan struct{}, 1)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errch:
			return err
		case <-exit:
			return nil
		case t := <-syncNotifier:
			a.showSync(t)
		case e := <-keyEvents:
			if e.Err != nil {
				return fmt.Errorf("key event failed: %w", e.Err)
			}
			go func() {
				if err := a.processKeyEvent(ctx, exit, e); err != nil {
					errch <- fmt.Errorf("process key event failed: %w", err)
				}
			}()
		}
	}

	return nil
}

func (a *App) processKeyEvent(ctx context.Context, exit chan struct{}, e keyboard.KeyEvent) error {
	if e.Key == keyboard.KeyCtrlC || e.Key == keyboard.KeyCtrlD || e.Rune == 'q' {
		exit <- struct{}{}
	}
	if e.Rune == 's' || e.Rune == 'd' {
		withDeletion := e.Rune == 'd'
		a.op("Syncing codespace to local")
		if err := a.syncer.SyncToLocal(ctx, withDeletion); err != nil {
			return fmt.Errorf("sync to local failed: %w", err)
		}
		a.opdone()
	}
	return nil
}

func (a *App) showSync(t syncType) {
	// Don't append a sync record if the spinner is active. Wait for it to stop.
	for a.spinner.Active() {
	}
	syncRecord := fmt.Sprintf("[INFO] Synced to %s on: %s\n", t, time.Now().Format(time.RFC1123))
	fmt.Fprintf(os.Stdout, syncRecord)
}

const availableCommands = `
Available commands
	s = sync to local
	d = sync to local w/ deletion
	q = quit
`

func (a *App) showAvailableCommands() {
	fmt.Println(availableCommands)
}

func (a *App) pickCodespace(ctx context.Context) (string, string, error) {
	a.op("Fetching codespaces")
	codespaces, err := ListCodespaces(ctx)
	a.opdone()
	if err != nil {
		return "", "", fmt.Errorf("list codespaces failed: %w", err)
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
		return "", "", fmt.Errorf("survey failed: %w", err)
	}
	codespace, ok := codespacesIndex[answers.Codespace]
	if !ok {
		return "", "", fmt.Errorf("codespace not found: %s", answers.Codespace)
	}
	return codespace.Name, codespace.Workspace(), nil
}

func (a *App) waitForSSHServer(ctx context.Context, errch chan error, s *sshServer) (string, error) {
	select {
	case err := <-errch:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	case username := <-s.Ready():
		return username, nil
	}
}

func (a *App) op(msg string) {
	a.spinner.Suffix = fmt.Sprintf(" %s", msg)
	a.spinner.Start()
}

func (a *App) opdone() {
	a.spinner.Stop()
}
