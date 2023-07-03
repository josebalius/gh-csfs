package csfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/briandowns/spinner"
	"github.com/eiannone/keyboard"
)

// App is the main application for csfs. It manages the user interaction
// and the sync operations.
type App struct {
	syncer   *syncer
	outputmu sync.Mutex
}

// NewApp creates a new App, with a spinner.
func NewApp() *App {
	return &App{}
}

// Run runs the application, it will have the user pick a codespace if none is provided.
// If the workspace cannot be computed from the codespace (rare and unexpected) it will
// return an error.
//
// The main flow is:
// 1. Start the SSH Server.
// 2. Wait for the SSH Server to be ready.
// 3. Setup the sync operations.
// 4. Sync the workspace dir to the current directory under the workspace dir name.
// 5. Start the file watcher and rsync on debounced changes, half a second.
// 6. Start the keyboard listener.
// 7. Wait for the user to press a key.
// 8. Process the key event.
// 9. If the key event is a quit key, exit.
// 10. If the key event is a sync key, sync the workspace dir to the current directory.
func (a *App) Run(
	ctx context.Context, codespaceName, workspace string, exclude []string, deleteFiles bool,
) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errch := make(chan error, 4) // sshServer, watcher, syncer, keyEvents
	defer close(errch)

	codespace, err := a.getOrChooseCodespace(ctx, codespaceName)
	if err != nil {
		return fmt.Errorf("get or choose codespace failed: %w", err)
	}
	if workspace == "" {
		if workspace = codespace.Workspace(); workspace == "" {
			return errors.New("workspace is required")
		}
	}

	// Start the SSH Server and wait for it to be ready,
	// timeout after 10 seconds, or if the server fails to start.
	var server *sshServer
	if err := a.op("Connecting to codespace", func() error {
		server = newSSHServer(codespace.Name)
		return nil
	}); err != nil {
		return fmt.Errorf("new ssh server failed: %w", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("ssh server close failed: %w", closeErr)
		}
	}()
	go func() {
		if err := server.Listen(ctx); err != nil {
			errch <- fmt.Errorf("ssh server failed: %w", err)
		}
	}()

	// Wait for the ssh server to be ready, or timeout.
	timeout := 10 * time.Second
	op := "Waiting for server to be ready"
	if !codespace.Active() {
		timeout = 120 * time.Second
		op = "Starting server, this may take a few minutes"
	}
	sshServerCtx, sshServerCancel := context.WithTimeout(ctx, timeout)
	defer sshServerCancel()

	var conn sshServerConn
	err = a.op(op, func() error {
		// TODO(josebalius): check status of the codespace, if is stopped, notify the user that the timeout
		// will be increased to 120 seconds, it'll never spin up in 10 seconds.
		conn, err = a.waitForSSHServer(sshServerCtx, errch, server)
		return err
	})
	if err != nil {
		if err == context.DeadlineExceeded {
			return errors.New("SSH Server timed out")
		}
		return fmt.Errorf("ssh server ready failed: %w", err)
	}

	err = a.op("Setting up sync opertions", func() error {
		a.syncer, err = a.setupSyncer(conn, workspace, exclude)
		return err
	})
	if err != nil {
		return fmt.Errorf("setup syncer failed: %w", err)
	}
	go func() {
		if err := a.syncer.Sync(ctx); err != nil {
			errch <- fmt.Errorf("sync failed: %w", err)
		}
	}()

	// Sync the workspace dir to the current directory. This sync omits
	// the .git directory.
	err = a.op("Syncing codespace to local", func() error {
		return a.syncer.SyncToLocal(ctx, deleteFiles)
	})
	if err != nil {
		return fmt.Errorf("sync to local failed: %w", err)
	}

	// Start the file watcher and rsync on debounced changes, half a second.
	var watcher *watcher
	err = a.op("Starting file watcher", func() error {
		watcher, err = newWatcher(a.syncer)
		return err
	})
	if err != nil {
		return fmt.Errorf("new watcher failed: %w", err)
	}
	go func() {
		if err := watcher.Watch(ctx); err != nil {
			errch <- fmt.Errorf("watcher failed: %w", err)
		}
	}()

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
		case e := <-a.syncer.Event():
			a.showSync(e)
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
}

func (a *App) setupSyncer(conn sshServerConn, workspace string, exclude []string) (*syncer, error) {
	codespaceDir := fmt.Sprintf("%s@localhost:/workspaces/%s", conn.Username, workspace)
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd failed: %w", err)
	}
	localDir := filepath.Join(wd, workspace)
	excludes := []string{".git"}
	if len(exclude) > 0 {
		excludes = append(excludes, exclude...)
	}
	a.syncer = newSyncer(conn.Port, localDir, codespaceDir, excludes, 500*time.Millisecond)
	return a.syncer, nil
}

const availableCommands = `
Available commands:
 s = sync to local
 d = sync to local w/ deletion
 q = quit
`

func (a *App) showAvailableCommands() {
	fmt.Println(availableCommands)
}

func (a *App) processKeyEvent(ctx context.Context, exit chan struct{}, e keyboard.KeyEvent) error {
	if e.Key == keyboard.KeyCtrlC || e.Key == keyboard.KeyCtrlD || e.Rune == 'q' {
		exit <- struct{}{}
	}
	if e.Key == keyboard.KeyEnter {
		a.outputmu.Lock()
		defer a.outputmu.Unlock()

		fmt.Fprintln(os.Stdout, "")
	}
	if e.Rune == 's' || e.Rune == 'd' {
		var withDeletion bool
		op := "Syncing codespace to local"
		if e.Rune == 'd' {
			op = "Syncing codespace to local w/ deletion"
			withDeletion = true
		}
		err := a.op(op, func() error {
			return a.syncer.SyncToLocal(ctx, withDeletion)
		})
		if err != nil {
			return fmt.Errorf("sync to local failed: %w", err)
		}
	}
	return nil
}

func (a *App) showSync(e syncType) {
	a.outputmu.Lock()
	defer a.outputmu.Unlock()

	// TODO(josebalius): Figure out how to not to collide with the spinner.
	syncRecord := fmt.Sprintf("[INFO][%s] Synced to %s\n", time.Now().Format(time.RFC1123), e)
	fmt.Fprintf(os.Stdout, syncRecord)
}

func (a *App) getOrChooseCodespace(ctx context.Context, codespace string) (Codespace, error) {
	if codespace == "" {
		c, err := a.pickCodespace(ctx)
		if err != nil {
			return c, fmt.Errorf("pick codespace failed: %w", err)
		}
		return c, nil
	}
	c, err := GetCodespace(ctx, codespace)
	if err != nil {
		return c, fmt.Errorf("get codespace failed: %w", err)
	}
	return c, nil
}

func (a *App) pickCodespace(ctx context.Context) (Codespace, error) {
	var codespaces []Codespace
	var err error
	err = a.op("Fetching codespaces", func() error {
		codespaces, err = ListCodespaces(ctx)
		return err
	})
	if err != nil {
		return Codespace{}, fmt.Errorf("list codespaces failed: %w", err)
	}
	var codespacesByName []string
	codespacesIndex := make(map[string]Codespace)
	for _, codespace := range codespaces {
		name := fmt.Sprintf("%s: %s", codespace.Repository, codespace.DisplayName)
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
		return Codespace{}, fmt.Errorf("survey failed: %w", err)
	}
	codespace, ok := codespacesIndex[answers.Codespace]
	if !ok {
		return Codespace{}, fmt.Errorf("codespace not found: %s", answers.Codespace)
	}
	return codespace, nil
}

func (a *App) waitForSSHServer(ctx context.Context, errch chan error, s *sshServer) (sshServerConn, error) {
	select {
	case err := <-errch:
		return sshServerConn{}, err
	case <-ctx.Done():
		return sshServerConn{}, ctx.Err()
	case conn := <-s.Ready():
		return conn, nil
	}
}

func (a *App) op(msg string, fn func() error) error {
	a.outputmu.Lock()
	defer a.outputmu.Unlock()

	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
	s.Suffix = fmt.Sprintf(" %s", msg)
	s.Start()
	defer s.Stop()

	return fn()
}
