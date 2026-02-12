package config

import (
	"context"
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 100 * time.Millisecond

func (m *Module) startWatcher(ctx context.Context) {
	if len(m.configFiles) == 0 {
		return
	}

	watcher, err := fsnotify.NewWatcher()
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

func (m *Module) watchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	defer func() { _ = watcher.Close() }()

	var debounce *time.Timer

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(debounceDelay, func() {
					if err := m.reload(); err != nil {
						slog.Error("failed to reload config", slog.Any("error", err))
					} else {
						slog.Info("config reloaded successfully")
					}
				})
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", slog.Any("error", err))
		}
	}
}
