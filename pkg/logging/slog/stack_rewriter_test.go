package slog

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
)

// zeroFrame returns an empty frame used when no specific caller is needed.
func zeroFrame() runtime.Frame { return runtime.Frame{} }

func TestStackRewriter_Enabled(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	r := newStackRewriter(context.Background(), upstream, zeroFrame())

	testza.AssertTrue(t, r.Enabled(context.Background(), slog.LevelDebug))
	testza.AssertTrue(t, r.Enabled(context.Background(), slog.LevelError))
}

func TestStackRewriter_Handle_PassesRecordToUpstream(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	r := newStackRewriter(context.Background(), upstream, zeroFrame())

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello from test", pcs[0])

	testza.AssertNil(t, r.Handle(context.Background(), rec))
	testza.AssertEqual(t, 1, len(upstream.records))
	testza.AssertEqual(t, "hello from test", upstream.records[0].Message)
}

func TestStackRewriter_WithAttrs_ReturnsStackRewriter(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	r := newStackRewriter(context.Background(), upstream, zeroFrame())

	h := r.WithAttrs([]slog.Attr{slog.String("k", "v")})
	_, ok := h.(stackRewriter)
	testza.AssertTrue(t, ok)
}

func TestStackRewriter_WithGroup_ReturnsStackRewriter(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	r := newStackRewriter(context.Background(), upstream, zeroFrame())

	h := r.WithGroup("grp")
	_, ok := h.(stackRewriter)
	testza.AssertTrue(t, ok)
}

func TestDetermineModule_Std_Runtime(t *testing.T) {
	t.Parallel()

	result := determineModule("/usr/local/go/src/runtime/panic.go", "runtime.panic", "", "", nil)
	testza.AssertEqual(t, "std", result)
}

func TestDetermineModule_Std_GoSrc(t *testing.T) {
	t.Parallel()

	result := determineModule("/home/user/go/src/fmt/print.go", "fmt.Println", "", "", nil)
	testza.AssertEqual(t, "std", result)
}

func TestDetermineModule_PkgMod(t *testing.T) {
	t.Parallel()

	result := determineModule(
		"/home/user/go/pkg/mod/github.com/samber/do/v2@v2.0.0/injector.go",
		"github.com/samber/do/v2.(*Injector).Invoke",
		"", "", nil,
	)
	testza.AssertEqual(t, "github.com/samber/do/v2@v2.0.0", result)
}

func TestDetermineModule_MainPackage(t *testing.T) {
	t.Parallel()

	modules := map[string]string{"github.com/example/app": "v1.0.0"}
	result := determineModule("/app/main.go", "main.main", "github.com/example/app", "v1.0.0", modules)
	testza.AssertEqual(t, "github.com/example/app@v1.0.0", result)
}

func TestDetermineModule_KnownModule(t *testing.T) {
	t.Parallel()

	modules := map[string]string{
		"github.com/example/app":     "v1.0.0",
		"github.com/example/app/pkg": "v1.0.0",
	}
	result := determineModule(
		"/workspace/pkg/handler.go",
		"github.com/example/app/pkg.(*Handler).Handle",
		"github.com/example/app", "v1.0.0", modules,
	)
	testza.AssertEqual(t, "github.com/example/app/pkg@v1.0.0", result)
}

func TestDetermineModule_Unknown_NoMatch(t *testing.T) {
	t.Parallel()

	result := determineModule("/tmp/foo.go", "some.Func", "", "", map[string]string{})
	testza.AssertEqual(t, unknownModule, result)
}

func TestDetermineModule_Unknown_NoLastDot(t *testing.T) {
	t.Parallel()

	result := determineModule("/tmp/foo.go", "nodotfuncname", "", "", map[string]string{})
	testza.AssertEqual(t, unknownModule, result)
}

func TestExtractModuleFromPkgMod_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want string
	}{
		{
			"/home/user/go/pkg/mod/github.com/samber/do/v2@v2.0.0/file.go",
			"github.com/samber/do/v2@v2.0.0",
		},
		{
			"/root/go/pkg/mod/github.com/foo/bar@v1.2.3/pkg/x.go",
			"github.com/foo/bar@v1.2.3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			testza.AssertEqual(t, tc.want, extractModuleFromPkgMod(tc.path))
		})
	}
}

func TestExtractModuleFromPkgMod_NoAtSign(t *testing.T) {
	t.Parallel()

	result := extractModuleFromPkgMod("/home/user/go/pkg/mod/github.com/foo/bar/pkg/x.go")
	testza.AssertEqual(t, unknownModule, result)
}

func TestExtractModuleFromPkgMod_InsufficientParts(t *testing.T) {
	t.Parallel()

	result := extractModuleFromPkgMod("/no/pkg/mod/separator/here.go")
	testza.AssertEqual(t, unknownModule, result)
}
