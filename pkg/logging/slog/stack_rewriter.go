package slog

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Vilsol/slox"
)

const DefaultDepth = 32

var _ slog.Handler = (*stackRewriter)(nil)

type stackRewriter struct {
	upstream          slog.Handler
	runtimeModule     string
	allModules        map[string]string
	mainModulePath    string
	mainModuleVersion string
}

func newStackRewriter(ctx context.Context, upstream slog.Handler, runtimeFrame runtime.Frame) stackRewriter {
	buildInfo, ok := debug.ReadBuildInfo()
	runtimeModule := ""
	allModules := make(map[string]string)
	mainModulePath := ""
	mainModuleVersion := ""

	if !ok {
		slox.Warn(ctx, "failed to retrieve build info, logged traces may be incorrect")
	} else {
		mainModulePath = buildInfo.Main.Path
		mainModuleVersion = buildInfo.Main.Version

		allModules[mainModulePath] = mainModuleVersion

		for _, dep := range buildInfo.Deps {
			allModules[dep.Path] = dep.Version
		}

		runtimeModule = determineModule(runtimeFrame.File, runtimeFrame.Function, mainModulePath, mainModuleVersion, allModules)
	}

	return stackRewriter{
		upstream:          upstream,
		runtimeModule:     runtimeModule,
		allModules:        allModules,
		mainModulePath:    mainModulePath,
		mainModuleVersion: mainModuleVersion,
	}
}

func (t stackRewriter) Enabled(ctx context.Context, level slog.Level) bool {
	return t.upstream.Enabled(ctx, level)
}

func (t stackRewriter) Handle(ctx context.Context, record slog.Record) error {
	fs := runtime.CallersFrames([]uintptr{record.PC})
	f, _ := fs.Next()

	valid := func(file string, function string) (bool, bool) {
		module := determineModule(file, function, t.mainModulePath, t.mainModuleVersion, t.allModules)
		return !strings.Contains(strings.ToLower(module), "github.com/!vilsol/slox"), module == t.runtimeModule
	}

	_, inPackage := valid(f.File, f.Function)
	if inPackage {
		return t.upstream.Handle(ctx, record) //nolint:wrapcheck
	}

	var pcs [DefaultDepth]uintptr
	runtime.Callers(0, pcs[:])

	start := 0
	for i, pc := range pcs {
		if pc == record.PC {
			start = i
		}
	}

	fs = runtime.CallersFrames(pcs[start:])

	changed := false
	f, _ = fs.Next()
	var noSloxF runtime.Frame
	for f.PC != 0 {
		noSlox, inPackage := valid(f.File, f.Function)
		if inPackage {
			record.PC = f.PC
			changed = true
			break
		}

		if noSlox && noSloxF.PC == 0 {
			noSloxF = f
			continue
		}

		f, _ = fs.Next()
	}

	if !changed && noSloxF.PC != 0 {
		record.PC = noSloxF.PC
	}

	return t.upstream.Handle(ctx, record) //nolint:wrapcheck
}

func (t stackRewriter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return stackRewriter{upstream: t.upstream.WithAttrs(attrs)}
}

func (t stackRewriter) WithGroup(name string) slog.Handler {
	return stackRewriter{upstream: t.upstream.WithGroup(name)}
}

func determineModule(filePath, funcName string, mainModule, mainVersion string, modules map[string]string) string {
	filePath = filepath.ToSlash(filePath)

	// Check if it's from the Go installation (standard library)
	if strings.Contains(filePath, "/src/runtime/") ||
		strings.Contains(filePath, "/go/src/") {
		return "std"
	}

	// Check if it's from go/pkg/mod (a dependency)
	if strings.Contains(filePath, "/go/pkg/mod/") {
		return extractModuleFromPkgMod(filePath)
	}

	// Handle main package functions
	if strings.HasPrefix(funcName, "main.") {
		return fmt.Sprintf("%s@%s", mainModule, mainVersion)
	}

	// Extract package path from function name
	// Function names are like: github.com/user/repo/pkg/sub.FuncName
	lastDot := strings.LastIndex(funcName, ".")
	if lastDot == -1 {
		return "unknown"
	}

	packagePath := funcName[:lastDot]

	// Find the longest matching module path
	longestMatch := ""
	matchedVersion := ""

	for modPath, version := range modules {
		if strings.HasPrefix(packagePath, modPath) {
			if len(modPath) > len(longestMatch) {
				longestMatch = modPath
				matchedVersion = version
			}
		}
	}

	if longestMatch != "" {
		return fmt.Sprintf("%s@%s", longestMatch, matchedVersion)
	}

	return "unknown"
}

func extractModuleFromPkgMod(filePath string) string {
	parts := strings.Split(filePath, "/go/pkg/mod/")
	if len(parts) < 2 {
		return "unknown"
	}

	remainder := parts[1]
	pathParts := strings.Split(remainder, "/")

	// Find the @version marker
	for i := 1; i <= len(pathParts); i++ {
		candidate := strings.Join(pathParts[:i], "/")
		if strings.Contains(candidate, "@") {
			return candidate
		}
	}

	return "unknown"
}
