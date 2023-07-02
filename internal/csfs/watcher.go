package csfs

import "context"

type watcher struct {
	syncer *syncer
}

func newWatcher(s *syncer) (*watcher, error) {
	return &watcher{s}, nil
}

func (w *watcher) Watch(ctx context.Context) error {
	return nil
}
