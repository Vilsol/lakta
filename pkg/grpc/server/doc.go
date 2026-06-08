// Package grpcserver provides a lakta module for running gRPC servers.
//
// Default keepalive parameters are applied: ServerParameters{MaxConnectionIdle 5m,
// Time 2h, Timeout 20s} and EnforcementPolicy{MinTime 30s, PermitWithoutStream true}.
// These are framework-level constants and are not yet koanf-configurable.
package grpcserver
