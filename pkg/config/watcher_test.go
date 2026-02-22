package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/fsnotify/fsnotify"
	"github.com/knadh/koanf/v2"
)

// mockFileWatcher is a controllable fileWatcher for tests.
type mockFileWatcher struct {
	events chan fsnotify.Event
	errors chan error
	addErr error
	Added  []string
}

func newMockWatcher() *mockFileWatcher {
	return &mockFileWatcher{
		events: make(chan fsnotify.Event, 10),
		errors: make(chan error, 10),
	}
}

func (m *mockFileWatcher) Add(name string) error {
	m.Added = append(m.Added, name)
	return m.addErr
}

func (m *mockFileWatcher) Events() <-chan fsnotify.Event { return m.events }
func (m *mockFileWatcher) Errors() <-chan error          { return m.errors }
func (m *mockFileWatcher) Close() error                  { return nil }

func waitChan(t *testing.T, ch <-chan struct{}, desc string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for: %s", desc)
	}
}

func TestWatchLoop_ContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	m := NewModule(WithConfigDirs("/nonexistent"))
	mock := newMockWatcher()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.watchLoop(ctx, mock)
	}()

	cancel()
	waitChan(t, done, "watchLoop to exit on context cancel")
}

func TestWatchLoop_WriteEventTriggersReload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m := NewModule(WithConfigDirs("/nonexistent"), WithDebounceDelay(1*time.Millisecond))
	mock := newMockWatcher()

	reloaded := make(chan struct{}, 1)
	m.onReload = append(m.onReload, func(_ *koanf.Koanf) {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})

	go m.watchLoop(ctx, mock)

	mock.events <- fsnotify.Event{Op: fsnotify.Write, Name: "lakta.yaml"}
	waitChan(t, reloaded, "reload to be triggered by Write event")
}

func TestWatchLoop_CreateEventTriggersReload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m := NewModule(WithConfigDirs("/nonexistent"), WithDebounceDelay(1*time.Millisecond))
	mock := newMockWatcher()

	reloaded := make(chan struct{}, 1)
	m.onReload = append(m.onReload, func(_ *koanf.Koanf) {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})

	go m.watchLoop(ctx, mock)

	mock.events <- fsnotify.Event{Op: fsnotify.Create, Name: "lakta.yaml"}
	waitChan(t, reloaded, "reload to be triggered by Create event")
}

func TestWatchLoop_IgnoredEventDoesNotReload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m := NewModule(WithConfigDirs("/nonexistent"), WithDebounceDelay(1*time.Millisecond))
	mock := newMockWatcher()

	reloaded := make(chan struct{}, 1)
	m.onReload = append(m.onReload, func(_ *koanf.Koanf) {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})

	go m.watchLoop(ctx, mock)

	mock.events <- fsnotify.Event{Op: fsnotify.Remove, Name: "lakta.yaml"}

	select {
	case <-reloaded:
		t.Fatal("reload should not be triggered by Remove event")
	case <-time.After(20 * time.Millisecond):
		// expected: no reload
	}
}

func TestWatchLoop_EventsChannelClosedExitsLoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m := NewModule(WithConfigDirs("/nonexistent"))
	mock := newMockWatcher()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.watchLoop(ctx, mock)
	}()

	close(mock.events)
	waitChan(t, done, "watchLoop to exit when events channel is closed")
}

func TestWatchLoop_ErrorsChannelClosedExitsLoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m := NewModule(WithConfigDirs("/nonexistent"))
	mock := newMockWatcher()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.watchLoop(ctx, mock)
	}()

	// Drain events so the select picks the errors case.
	close(mock.errors)
	waitChan(t, done, "watchLoop to exit when errors channel is closed")
}

func TestWatchLoop_WatcherErrorContinues(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	m := NewModule(WithConfigDirs("/nonexistent"))
	mock := newMockWatcher()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.watchLoop(ctx, mock)
	}()

	mock.errors <- errors.New("disk error")

	// After an error the loop should still be running.
	select {
	case <-done:
		t.Fatal("watchLoop should not exit on a watcher error")
	case <-time.After(20 * time.Millisecond):
		// still running — correct
	}

	cancel()
	waitChan(t, done, "watchLoop to exit after context cancel")
}

func TestWatchLoop_DebounceDeduplication(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m := NewModule(WithConfigDirs("/nonexistent"), WithDebounceDelay(20*time.Millisecond))
	mock := newMockWatcher()

	reloadCount := 0
	reloaded := make(chan struct{}, 5)
	m.onReload = append(m.onReload, func(_ *koanf.Koanf) {
		reloadCount++
		reloaded <- struct{}{}
	})

	go m.watchLoop(ctx, mock)

	// Rapid-fire three events — debounce should collapse them into one reload.
	mock.events <- fsnotify.Event{Op: fsnotify.Write, Name: "lakta.yaml"}
	mock.events <- fsnotify.Event{Op: fsnotify.Write, Name: "lakta.yaml"}
	mock.events <- fsnotify.Event{Op: fsnotify.Write, Name: "lakta.yaml"}

	waitChan(t, reloaded, "at least one reload")

	// Wait for any possible second reload.
	time.Sleep(60 * time.Millisecond)

	testza.AssertEqual(t, 1, reloadCount)
}

func TestStartWatcher_NoConfigFilesSkipsWatcher(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	called := false
	m := NewModule(WithConfigDirs("/nonexistent"))
	m.watcherFactory = func() (fileWatcher, error) {
		called = true
		return newMockWatcher(), nil
	}

	// No config files → startWatcher returns without calling factory.
	m.startWatcher(ctx)
	testza.AssertFalse(t, called)
}

func TestStartWatcher_WithConfigFileCallsFactory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("x: 1\n"), 0o600))

	ctx, cancel := context.WithCancel(setupModuleCtx(t))
	defer cancel()

	mock := newMockWatcher()
	m := NewModule(WithConfigDirs(dir))
	m.watcherFactory = func() (fileWatcher, error) { return mock, nil }

	// Init discovers the file, then startWatcher calls the factory.
	testza.AssertNil(t, m.Init(ctx))

	testza.AssertEqual(t, 1, len(mock.Added))
}

func TestStartWatcher_FactoryErrorDoesNotFailInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("x: 1\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir))
	m.watcherFactory = func() (fileWatcher, error) {
		return nil, errors.New("watcher unavailable")
	}

	// Factory failure is only a warning — Init must still succeed.
	testza.AssertNil(t, m.Init(ctx))
}

func TestStartWatcher_AddErrorDoesNotFailInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("x: 1\n"), 0o600))

	ctx := setupModuleCtx(t)
	mock := newMockWatcher()
	mock.addErr = errors.New("permission denied")

	m := NewModule(WithConfigDirs(dir))
	m.watcherFactory = func() (fileWatcher, error) { return mock, nil }

	// Add failure is only a warning — Init must still succeed.
	testza.AssertNil(t, m.Init(ctx))
}
