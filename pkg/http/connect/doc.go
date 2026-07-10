// Package connect provides a Connect-RPC [Module]: one net/http + h2c handler
// serving Connect (JSON/proto over HTTP), gRPC-Web, and gRPC on a single port,
// so existing gRPC clients keep working unchanged while HTTP/JSON callers hit the
// identical service. It is the recommended default lakta API workflow;
// pkg/grpc/server is retained as the pure grpc-go / xDS escape hatch.
//
// Services register via [WithService], the deferred DI-style registrar that
// receives the assembled shared interceptor chain (otelconnect, logging,
// recovery, error rendering, protovalidate) to spread into a connect
// NewXxxHandler constructor. Errors render through the pkg/errors AppError wire
// contract, so a protovalidate CEL violation returns the same
// errdetails.BadRequest a gRPC caller sees.
//
// This is an own go-module (its connect/otelconnect/protovalidate deps must
// not enter the core graph). Until the next lakta tag the sibling
// require github.com/Vilsol/lakta resolves via the workspace replace directive
// (go.work: replace github.com/Vilsol/lakta => .).
package connect
