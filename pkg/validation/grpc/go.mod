module github.com/Vilsol/lakta/pkg/validation/grpc

go 1.26.4

// Sibling modules (github.com/Vilsol/lakta core, google.golang.org/grpc) resolve
// via go.work until the next tag; buf.build/go/protovalidate is promoted to a
// direct, module-scoped dependency here so it stays out of the root module graph.
require buf.build/go/protovalidate v1.2.0
