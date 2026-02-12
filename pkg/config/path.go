package config

import "fmt"

// Category constants for module organization.
const (
	CategoryOTel      = "otel"
	CategoryLogging   = "logging"
	CategoryHealth    = "health"
	CategoryHTTP      = "http"
	CategoryGRPC      = "grpc"
	CategoryDB        = "db"
	CategoryWorkflows = "workflows"
)

// DefaultInstanceName is the default instance name for modules.
const DefaultInstanceName = "default"

// ModulePath generates the config path for a module instance.
// Example: ModulePath("grpc", "server", "internal") -> "modules.grpc.server.internal"
func ModulePath(category, moduleType, instance string) string {
	if instance == "" {
		instance = DefaultInstanceName
	}
	return fmt.Sprintf("modules.%s.%s.%s", category, moduleType, instance)
}
