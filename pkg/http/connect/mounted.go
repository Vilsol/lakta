package connect

import (
	"context"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// MountedService describes a protobuf service mounted on the server.
type MountedService struct {
	Path       string
	Descriptor protoreflect.ServiceDescriptor
}

// MountedHook runs in Init after every service is mounted (interceptors are
// built before registrars run, so slot owners learn the services here). An
// error fails Init.
type MountedHook func(ctx context.Context, services []MountedService) error

// WithMountedHook registers a hook that receives the mounted services.
func WithMountedHook(fn MountedHook) Option {
	return func(m *Config) { m.MountedHooks = append(m.MountedHooks, fn) }
}

// resolveMounted maps mux paths ("/pkg.Service/") to service descriptors from
// the global registry; paths that are not protobuf services are skipped.
func resolveMounted(paths []string) []MountedService {
	services := make([]MountedService, 0, len(paths))
	for _, path := range paths {
		name := protoreflect.FullName(strings.Trim(path, "/"))
		if !name.IsValid() {
			continue
		}
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
		if err != nil {
			continue
		}
		if sd, ok := d.(protoreflect.ServiceDescriptor); ok {
			services = append(services, MountedService{Path: path, Descriptor: sd})
		}
	}
	return services
}

// procedures maps every "/pkg.Service/Method" path to whether the RPC is unary.
func procedures(services []MountedService) map[string]bool {
	result := make(map[string]bool)
	for _, s := range services {
		methods := s.Descriptor.Methods()
		for i := range methods.Len() {
			md := methods.Get(i)
			result["/"+string(s.Descriptor.FullName())+"/"+string(md.Name())] = !md.IsStreamingClient() && !md.IsStreamingServer()
		}
	}
	return result
}
