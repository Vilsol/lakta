module github.com/Vilsol/lakta

go 1.26.0

require (
	github.com/Masterminds/squirrel v1.5.4
	github.com/Vilsol/slox v0.0.1
	github.com/exaring/otelpgx v0.10.0
	github.com/fsnotify/fsnotify v1.9.0
	github.com/gofiber/contrib/v3/otel v1.0.0
	github.com/gofiber/fiber/v3 v3.0.0
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.3
	github.com/hellofresh/health-go/v5 v5.5.5
	github.com/jackc/pgx/v5 v5.8.0
	github.com/knadh/koanf/parsers/json v1.0.0
	github.com/knadh/koanf/parsers/toml/v2 v2.2.0
	github.com/knadh/koanf/parsers/yaml v1.1.0
	github.com/knadh/koanf/providers/env v1.1.0
	github.com/knadh/koanf/providers/file v1.2.1
	github.com/knadh/koanf/providers/posflag v1.0.1
	github.com/knadh/koanf/v2 v2.3.2
	github.com/lmittmann/tint v1.1.3
	github.com/mitchellh/mapstructure v1.5.0
	github.com/remychantenay/slog-otel v1.3.5
	github.com/samber/do/v2 v2.0.0
	github.com/samber/oops v1.21.0
	github.com/samber/slog-multi v1.7.1
	github.com/sourcegraph/conc v0.3.0
	github.com/spf13/pflag v1.0.10
	go.opentelemetry.io/contrib/bridges/otelslog v0.15.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.65.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.65.0
	go.opentelemetry.io/otel v1.40.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.16.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.40.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.40.0
	go.opentelemetry.io/otel/log v0.16.0
	go.opentelemetry.io/otel/sdk v1.40.0
	go.opentelemetry.io/otel/sdk/log v0.16.0
	go.opentelemetry.io/otel/sdk/metric v1.40.0
	go.opentelemetry.io/otel/trace v1.40.0
	go.temporal.io/sdk v1.39.0
	go.temporal.io/sdk/contrib/opentelemetry v0.6.0
	golang.org/x/sync v0.19.0
	google.golang.org/grpc v1.78.0
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/gofiber/schema v1.7.0 // indirect
	github.com/gofiber/utils/v2 v2.0.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/lann/builder v0.0.0-20180802200727-47ae307949d0 // indirect
	github.com/lann/ps v0.0.0-20150810152359-62de8c46ede0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/nexus-rpc/sdk-go v0.5.1 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rivo/uniseg v0.4.3 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/samber/go-type-to-string v1.8.0 // indirect
	github.com/samber/lo v1.52.0 // indirect
	github.com/samber/slog-common v0.20.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/tinylib/msgp v1.6.3 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.69.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.temporal.io/api v1.59.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.9.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
