package slog

import "log/slog"

// LevelController is a runtime-settable handle over the module's default log
// level, provided into DI for consumers such as the actuator's POST /loggers.
// It delegates to the existing level filter so per-package rules survive a
// default-level change (a bare *slog.LevelVar would bypass those rules).
type LevelController interface {
	SetLevel(level slog.Level)
	Level() slog.Level
}

// SetLevel swaps the default level, preserving current per-package overrides.
func (m *Module) SetLevel(level slog.Level) {
	m.levelMu.Lock()
	defer m.levelMu.Unlock()

	m.config.levelParsed = level
	m.filter.Update(level, m.config.levelsParsed)
}

// Level returns the current default level.
func (m *Module) Level() slog.Level {
	m.levelMu.Lock()
	defer m.levelMu.Unlock()

	return m.config.levelParsed
}
