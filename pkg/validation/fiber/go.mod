module github.com/Vilsol/lakta/pkg/validation/fiber

go 1.26.4

// Sibling modules (github.com/Vilsol/lakta core, gofiber/fiber/v3) resolve via
// go.work until the next tag; only the module-scoped validator/v10 is required
// here so it stays out of the root module graph.
require github.com/go-playground/validator/v10 v10.30.3

require (
	github.com/MarvinJWendt/testza v0.5.2
	github.com/Vilsol/lakta v0.4.0
	github.com/Vilsol/lakta/pkg/errors/fiber v0.4.0
	github.com/gofiber/fiber/v3 v3.4.0
)

require (
	atomicgo.dev/assert v0.0.2 // indirect
	atomicgo.dev/cursor v0.2.0 // indirect
	atomicgo.dev/keyboard v0.2.10 // indirect
	atomicgo.dev/schedule v0.1.0 // indirect
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/containerd/console v1.0.5 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/gofiber/schema v1.8.0 // indirect
	github.com/gofiber/utils/v2 v2.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gookit/color v1.6.1 // indirect
	github.com/klauspost/compress v1.19.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lithammer/fuzzysearch v1.1.8 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pterm/pterm v0.12.83 // indirect
	github.com/samber/lo v1.53.0 // indirect
	github.com/samber/oops v1.22.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.72.0 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/term v0.44.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/grpc v1.82.0 // indirect
)
