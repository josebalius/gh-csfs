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

func newWatcher(s *syncer, watch []string) (*watcher, error) {
	excludedPathsSet := excludedPathsSet(s.localDir, s.excludes)
	hasWatch, includedPathsSet := includedPathsSet(s.localDir, watch)
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
			if hasWatch {
				// Skip directories that are not in the watch list.
				if _, ok := includedPathsSet[newPath]; !ok {
					return filepath.SkipDir
				}
			}
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

func excludedPathsSet(dir string, excludes []string) map[string]struct{} {
	excludedPathsSet := make(map[string]struct{})
	for _, exclude := range excludes {
		if exclude[0] != '/' {
			exclude = path.Join(dir, exclude)
		}
		excludedPathsSet[exclude] = struct{}{}
	}
	return excludedPathsSet
}

func includedPathsSet(dir string, included []string) (bool, map[string]struct{}) {
	if len(included) == 0 {
		return false, nil
	}
	includePathsSet := make(map[string]struct{})
	for _, include := range included {
		if include[0] != '/' {
			include = path.Join(dir, include)
		}
		includePathsSet[include] = struct{}{}
	}
	return true, includePathsSet
}

func (w *watcher) Watch(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.watcher.Events:
			w.syncer.SyncToCodespace(ctx)
		case err := <-w.watcher.Errors:
			return err
		}
	}
	return nil
}
