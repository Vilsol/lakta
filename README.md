# lakta

An opinionated golang microservice framework.

## Libraries

| Purpose              | Name          | Library                              |
|----------------------|---------------|--------------------------------------|
| Log API              | slog          | `log/slog`                           |
| Log Formatter        | tint          | `github.com/lmittmann/tint`          |
| Dependency Injection | Do            | `github.com/samber/do/v2`            |
| Configuration        | Koanf         | `github.com/knadh/koanf`             |
| Logging              | OpenTelemetry | `go.opentelemetry.io/otel/log`       |
| Metrics              | OpenTelemetry | `go.opentelemetry.io/otel/metric`    |
| Tracing              | OpenTelemetry | `go.opentelemetry.io/otel/trace`     |
| HTTP Server          | Fiber         | `github.com/gofiber/fiber/v3`        |
| GRPC Server          | Google GRPC   | `google.golang.org/grpc`             |
| SQL Database         | squirrel      | `github.com/Masterminds/squirrel`    |
| Postgres Database    | pgx           | `github.com/jackc/pgx/v5`            |
| Health Check         | health-go     | `github.com/hellofresh/health-go/v5` |
| Testing              | testza        | `github.com/MarvinJWendt/testza`     |
| Error Handling       | oops          | `github.com/samber/oops`             |
| Goroutine Management | conc          | `github.com/sourcegraph/conc`        |
