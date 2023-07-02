package csfs

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type watcher struct {
	watcher *fsnotify.Watcher
	syncer  *syncer
}

func newWatcher(s *syncer) (*watcher, error) {
	excludedPathsSet := make(map[string]struct{})
	for _, exclude := range s.excludes {
		if exclude[0] != '/' {
			exclude = path.Join(s.localDir, exclude)
		}
		excludedPathsSet[exclude] = struct{}{}
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	// Recursively travel tree, and collect directories to watch.
	err = filepath.Walk(s.localDir, func(newPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded paths
		if _, ok := excludedPathsSet[newPath]; ok {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			err = w.Add(newPath)
			if err != nil {
				return fmt.Errorf("failed to add %s to watcher: %w", newPath, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk %s: %w", s.localDir, err)
	}

	return &watcher{syncer: s, watcher: w}, nil
}

func (w *watcher) Watch(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.watcher.Events:
			w.syncer.SyncToCodespace(ctx)
		case err := <-w.watcher.Errors:
			return err
		}
	}
	return nil
}
