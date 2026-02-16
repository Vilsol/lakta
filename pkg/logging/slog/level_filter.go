package slog

import (
	"context"
	"log/slog"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var _ slog.Handler = (*levelFilter)(nil)

type levelRule struct {
	prefix string
	level  slog.Level
}

type levelRules struct {
	defaultLevel slog.Level
	rules        []levelRule // sorted by prefix length descending
	minLevel     slog.Level  // minimum across default + all overrides
}

type levelFilter struct {
	upstream slog.Handler
	state    atomic.Pointer[levelRules]
	cache    sync.Map // funcName -> slog.Level
}

func buildRules(defaultLevel slog.Level, levels map[string]slog.Level) *levelRules {
	rules := make([]levelRule, 0, len(levels))
	minLevel := defaultLevel

	for prefix, level := range levels {
		rules = append(rules, levelRule{prefix: prefix, level: level})
		if level < minLevel {
			minLevel = level
		}
	}

	sort.Slice(rules, func(i, j int) bool {
		return len(rules[i].prefix) > len(rules[j].prefix)
	})

	return &levelRules{
		defaultLevel: defaultLevel,
		rules:        rules,
		minLevel:     minLevel,
	}
}

func newLevelFilter(upstream slog.Handler, defaultLevel slog.Level, levels map[string]slog.Level) *levelFilter {
	f := &levelFilter{upstream: upstream}
	f.state.Store(buildRules(defaultLevel, levels))
	return f
}

// Update atomically swaps the level rules and clears the resolution cache.
func (f *levelFilter) Update(defaultLevel slog.Level, levels map[string]slog.Level) {
	f.state.Store(buildRules(defaultLevel, levels))
	f.cache.Clear()
}

// Enabled returns true if the level could pass any rule.
// Can't do per-package filtering here since Enabled has no PC info.
func (f *levelFilter) Enabled(ctx context.Context, level slog.Level) bool {
	if override, ok := LogLevelFromContext(ctx); ok {
		if level >= override {
			return f.upstream.Enabled(ctx, level)
		}
	}

	s := f.state.Load()
	return level >= s.minLevel && f.upstream.Enabled(ctx, level)
}

func (f *levelFilter) Handle(ctx context.Context, record slog.Record) error {
	if override, ok := LogLevelFromContext(ctx); ok {
		if record.Level >= override {
			return f.upstream.Handle(ctx, record) //nolint:wrapcheck
		}
		return nil
	}

	threshold := f.resolveLevel(record.PC)
	if record.Level < threshold {
		return nil
	}

	return f.upstream.Handle(ctx, record) //nolint:wrapcheck
}

func (f *levelFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := &levelFilter{
		upstream: f.upstream.WithAttrs(attrs),
	}
	clone.state.Store(f.state.Load())
	return clone
}

func (f *levelFilter) WithGroup(name string) slog.Handler {
	clone := &levelFilter{
		upstream: f.upstream.WithGroup(name),
	}
	clone.state.Store(f.state.Load())
	return clone
}

func (f *levelFilter) resolveLevel(pc uintptr) slog.Level {
	fs := runtime.CallersFrames([]uintptr{pc})
	frame, _ := fs.Next()
	funcName := frame.Function

	if cached, ok := f.cache.Load(funcName); ok {
		level, _ := cached.(slog.Level)
		return level
	}

	pkgPath := extractPackagePath(funcName)
	s := f.state.Load()
	level := matchLevel(s, pkgPath)
	f.cache.Store(funcName, level)

	return level
}

// extractPackagePath returns the package import path from a fully qualified function name.
// e.g. "github.com/org/repo/pkg.(*Type).Method" -> "github.com/org/repo/pkg"
func extractPackagePath(funcName string) string {
	if funcName == "" {
		return ""
	}

	lastSlash := strings.LastIndex(funcName, "/")
	if lastSlash == -1 {
		before, _, found := strings.Cut(funcName, ".")
		if !found {
			return funcName
		}
		return before
	}

	// Find first dot after last slash
	rest := funcName[lastSlash+1:]
	before, _, found := strings.Cut(rest, ".")
	if !found {
		return funcName
	}

	return funcName[:lastSlash+1+len(before)]
}

func matchLevel(s *levelRules, pkgPath string) slog.Level {
	for _, rule := range s.rules {
		if strings.HasPrefix(pkgPath, rule.prefix) {
			return rule.level
		}
	}
	return s.defaultLevel
}
