package config

import (
	"context"
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/samber/oops"
)

// fileWatcher abstracts fsnotify.Watcher so the watcher can be replaced in tests.
type fileWatcher interface {
	Add(name string) error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Close() error
}

type fsnotifyAdapter struct{ w *fsnotify.Watcher }

func (a *fsnotifyAdapter) Add(name string) error         { return oops.Wrapf(a.w.Add(name), "fsnotify add") }
func (a *fsnotifyAdapter) Events() <-chan fsnotify.Event { return a.w.Events }
func (a *fsnotifyAdapter) Errors() <-chan error          { return a.w.Errors }
func (a *fsnotifyAdapter) Close() error                  { return oops.Wrapf(a.w.Close(), "fsnotify close") }

//nolint:ireturn
func defaultWatcherFactory() (fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, oops.Wrapf(err, "failed to create fsnotify watcher")
	}
	return &fsnotifyAdapter{w: w}, nil
}

func (m *Module) startWatcher(ctx context.Context) {
	if len(m.configFiles) == 0 {
		return
	}

	watcher, err := m.watcherFactory()
	if err != nil {
		slog.Warn("failed to create config file watcher", slog.Any("error", err))
		return
	}

	for _, cf := range m.configFiles {
		if err := watcher.Add(cf.path); err != nil {
			slog.Warn("failed to watch config file", slog.String("path", cf.path), slog.Any("error", err))
		}
	}

	go m.watchLoop(ctx, watcher)
}

func (m *Module) watchLoop(ctx context.Context, watcher fileWatcher) {
	defer func() { _ = watcher.Close() }()

	var debounce *time.Timer

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return

		case event, ok := <-watcher.Events():
			if !ok {
				return
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(m.config.DebounceDelay, func() {
					if err := m.reload(); err != nil {
						slog.Error("failed to reload config", slog.Any("error", err))
					} else {
						slog.Info("config reloaded successfully")
					}
				})
			}

		case err, ok := <-watcher.Errors():
			if !ok {
				return
			}
			slog.Error("config watcher error", slog.Any("error", err))
		}
	}
}
