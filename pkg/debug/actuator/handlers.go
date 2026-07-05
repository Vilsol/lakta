package actuator

import (
	"bytes"
	"expvar"
	"fmt"
	"log/slog"
	"net/http"
	nethttppprof "net/http/pprof"
	"runtime/debug"
	runtimepprof "runtime/pprof"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/samber/oops"
)

// registerHandlers mounts every enabled endpoint under the given (BasePath)
// router, wrapping sensitive ones (per Config.RequiresAuth) with the auth
// middleware — enforced on any bind, incl. loopback.
func (m *Module) registerHandlers(r fiber.Router) {
	authMW := m.authMiddleware()

	get := func(path string, h fiber.Handler) {
		if m.config.RequiresAuth(path) {
			r.Get(path, authMW, h)
			return
		}
		r.Get(path, h)
	}

	get(epModules, m.handleModules)
	get(epStartup, m.handleStartup)
	get(epConfig, m.handleConfig)
	get(epRoutes, m.handleRoutes)
	get(epInfo, m.handleInfo)
	get(epHealth, m.handleHealth)
	get(epDI, m.handleDI)
	get(epGoroutine, m.handleGoroutine)

	if m.config.Endpoints.Expvar {
		get(epVars, m.handleExpvar)
	}
	if m.config.Endpoints.Pprof {
		m.registerPprof(r, authMW)
	}
	if m.config.Endpoints.Loggers {
		r.Post(epLoggers, authMW, m.handleSetLoggers)
	}
}

// writeJSON encodes data as the JSON response, wrapping any encode error.
func writeJSON(c fiber.Ctx, data any) error {
	return oops.Wrapf(c.JSON(data), "failed to encode response")
}

// --- /modules ---

// ModulesResponse is the /modules JSON contract.
type ModulesResponse struct {
	Modules []ModuleView `json:"modules"`
}

// ModuleView is one module's rendered metadata.
type ModuleView struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	InitOrder    int      `json:"init_order"`
	Provides     []string `json:"provides"`
	Requires     []string `json:"requires"`
	Optional     []string `json:"optional"`
	Lifecycle    string   `json:"lifecycle"`
	State        string   `json:"state"`
	InitDuration string   `json:"init_duration"`
}

func (m *Module) handleModules(c fiber.Ctx) error {
	if m.runtimeInfo == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "runtime info unavailable")
	}

	snap := m.runtimeInfo.Snapshot()
	views := make([]ModuleView, 0, len(snap))
	for _, mi := range snap {
		views = append(views, ModuleView{
			Name:         mi.Name,
			Type:         mi.Type,
			InitOrder:    mi.InitOrder,
			Provides:     mi.Provides,
			Requires:     mi.Requires,
			Optional:     mi.Optional,
			Lifecycle:    mi.Lifecycle.String(),
			State:        mi.State.String(),
			InitDuration: mi.InitDuration.String(),
		})
	}

	return writeJSON(c, ModulesResponse{Modules: views})
}

// --- /startup ---

// StartupResponse is the /startup JSON contract (init waterfall).
type StartupResponse struct {
	Total   string         `json:"total_init_duration"`
	Entries []StartupEntry `json:"entries"`
}

// StartupEntry is one module's init timing.
type StartupEntry struct {
	Name         string `json:"name"`
	InitOrder    int    `json:"init_order"`
	InitDuration string `json:"init_duration"`
}

func (m *Module) handleStartup(c fiber.Ctx) error {
	if m.runtimeInfo == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "runtime info unavailable")
	}

	snap := m.runtimeInfo.Snapshot()
	entries := make([]StartupEntry, 0, len(snap))
	var total time.Duration
	for _, mi := range snap {
		total += mi.InitDuration
		name := mi.Name
		if name == "" {
			name = mi.Type
		}
		entries = append(entries, StartupEntry{
			Name:         name,
			InitOrder:    mi.InitOrder,
			InitDuration: mi.InitDuration.String(),
		})
	}

	return writeJSON(c, StartupResponse{Total: total.String(), Entries: entries})
}

// --- /config ---

// ConfigResponse is the /config JSON contract.
type ConfigResponse struct {
	Values     map[string]any    `json:"values"`
	Provenance map[string]string `json:"provenance,omitempty"`
}

func (m *Module) handleConfig(c fiber.Ctx) error {
	if m.koanf == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "config unavailable")
	}

	resp := ConfigResponse{Values: m.redactor.Redact(m.koanf.Raw(), false)}

	if c.Query("provenance") == "1" && m.configModule != nil {
		prov := make(map[string]string)
		for _, e := range m.configModule.ProvenanceSnapshot() {
			prov[e.Key] = e.Origin
		}
		resp.Provenance = prov
	}

	return writeJSON(c, resp)
}

// --- /routes ---

// RoutesResponse is the /routes JSON contract.
type RoutesResponse struct {
	Instances []InstanceRoutes `json:"instances"`
}

// InstanceRoutes is one fiber instance's routes.
type InstanceRoutes struct {
	Instance string      `json:"instance"`
	Routes   []RouteView `json:"routes"`
}

// RouteView is one registered route.
type RouteView struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
}

func (m *Module) handleRoutes(c fiber.Ctx) error {
	instances := make([]InstanceRoutes, 0)
	if m.routes != nil {
		for _, s := range m.routes.Snapshot() {
			rv := make([]RouteView, 0, len(s.Routes))
			for _, rt := range s.Routes {
				rv = append(rv, RouteView{Method: rt.Method, Path: rt.Path, Name: rt.Name})
			}
			instances = append(instances, InstanceRoutes{Instance: s.Instance, Routes: rv})
		}
	}

	return writeJSON(c, RoutesResponse{Instances: instances})
}

// --- /info ---

// InfoResponse is the /info JSON contract.
type InfoResponse struct {
	GoVersion string            `json:"go_version"`
	Path      string            `json:"path"`
	Main      ModuleVersion     `json:"main"`
	Settings  map[string]string `json:"settings"`
	Deps      []ModuleVersion   `json:"deps"`
}

// ModuleVersion is a build-info module path/version pair.
type ModuleVersion struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func (m *Module) handleInfo(c fiber.Ctx) error {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return fiber.NewError(fiber.StatusNotImplemented, "build info unavailable")
	}

	rawSettings := make(map[string]any, len(bi.Settings))
	for _, s := range bi.Settings {
		rawSettings[s.Key] = s.Value
	}
	redacted := m.redactor.Redact(rawSettings, false)
	settings := make(map[string]string, len(redacted))
	for k, v := range redacted {
		settings[k] = fmt.Sprint(v)
	}

	deps := make([]ModuleVersion, 0, len(bi.Deps))
	for _, d := range bi.Deps {
		deps = append(deps, ModuleVersion{Path: d.Path, Version: d.Version})
	}

	return writeJSON(c, InfoResponse{
		GoVersion: bi.GoVersion,
		Path:      bi.Path,
		Main:      ModuleVersion{Path: bi.Main.Path, Version: bi.Main.Version},
		Settings:  settings,
		Deps:      deps,
	})
}

// --- /health ---

func (m *Module) handleHealth(c fiber.Ctx) error {
	if m.health == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "health unavailable")
	}
	return adaptor.HTTPHandlerFunc(m.health.HandlerFunc)(c)
}

// --- /goroutine (auth-gated) ---

func (m *Module) handleGoroutine(c fiber.Ctx) error {
	var buf bytes.Buffer
	if err := runtimepprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		return oops.Wrapf(err, "failed to write goroutine profile")
	}
	c.Type("txt")
	return oops.Wrapf(c.SendString(buf.String()), "failed to write response")
}

// --- /vars (auth-gated expvar) ---

func (m *Module) handleExpvar(c fiber.Ctx) error {
	return adaptor.HTTPHandler(expvar.Handler())(c)
}

// --- /pprof/* (auth-gated, duration-capped) ---

func (m *Module) registerPprof(r fiber.Router, authMW fiber.Handler) {
	r.Get("/pprof/", authMW, adaptor.HTTPHandlerFunc(nethttppprof.Index))
	r.Get("/pprof/cmdline", authMW, adaptor.HTTPHandlerFunc(nethttppprof.Cmdline))
	r.Get("/pprof/symbol", authMW, adaptor.HTTPHandlerFunc(nethttppprof.Symbol))
	r.Post("/pprof/symbol", authMW, adaptor.HTTPHandlerFunc(nethttppprof.Symbol))
	r.Get("/pprof/profile", authMW, m.handleProfile)
	r.Get("/pprof/trace", authMW, m.handleTrace)
	r.Get("/pprof/:name", authMW, m.handleNamedProfile)
}

func (m *Module) handleProfile(c fiber.Ctx) error {
	if err := rejectOverCap(c); err != nil {
		return err
	}
	return adaptor.HTTPHandler(http.HandlerFunc(nethttppprof.Profile))(c)
}

func (m *Module) handleTrace(c fiber.Ctx) error {
	if err := rejectOverCap(c); err != nil {
		return err
	}
	return adaptor.HTTPHandler(http.HandlerFunc(nethttppprof.Trace))(c)
}

func (m *Module) handleNamedProfile(c fiber.Ctx) error {
	return adaptor.HTTPHandler(nethttppprof.Handler(c.Params("name")))(c)
}

// rejectOverCap enforces the pprof duration cap server-side: a ?seconds= above
// maxProfileSeconds is rejected so the client cannot request an unbounded CPU
// profile (DoS vector).
func rejectOverCap(c fiber.Ctx) error {
	sec := c.Query("seconds")
	if sec == "" {
		return nil
	}
	n, err := strconv.Atoi(sec)
	if err != nil {
		return nil
	}
	if clampProfileSeconds(n) != n {
		return fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("seconds=%d exceeds max %d", n, maxProfileSeconds))
	}
	return nil
}

// --- /di ---

// DIResponse is the /di JSON contract.
type DIResponse struct {
	Format string `json:"format"`
	Graph  string `json:"graph"`
}

func (m *Module) handleDI(c fiber.Ctx) error {
	if m.injector == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "injector unavailable")
	}

	format := c.Query("format", "mermaid")
	if format != "dot" {
		format = "mermaid"
	}

	return writeJSON(c, DIResponse{Format: format, Graph: renderDIGraph(m.injector, format)})
}

// --- POST /loggers (auth unconditional) ---

// LoggersRequest is the POST /loggers JSON body.
type LoggersRequest struct {
	Level string `json:"level"`
}

// LoggersResponse reports the effective default level after a change.
type LoggersResponse struct {
	Level string `json:"level"`
}

func (m *Module) handleSetLoggers(c fiber.Ctx) error {
	if m.levelController == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "level controller unavailable")
	}

	level := c.FormValue("level")
	if level == "" {
		var req LoggersRequest
		_ = c.Bind().Body(&req)
		level = req.Level
	}

	parsed, ok := parseLogLevel(level)
	if !ok {
		return fiber.NewError(fiber.StatusBadRequest, "invalid level: "+level)
	}

	m.levelController.SetLevel(parsed)

	return writeJSON(c, LoggersResponse{Level: levelString(m.levelController.Level())})
}

func parseLogLevel(s string) (slog.Level, bool) {
	switch s {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

func levelString(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return "debug"
	case l <= slog.LevelInfo:
		return "info"
	case l <= slog.LevelWarn:
		return "warn"
	default:
		return "error"
	}
}
