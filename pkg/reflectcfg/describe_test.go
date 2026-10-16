package reflectcfg_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/reflectcfg"
)

type serverBlock struct {
	ReadTimeout time.Duration `koanf:"read_timeout"`
}

type routeConfig struct {
	Path string `koanf:"path"`
}

type describedServer struct {
	Port         uint16        `koanf:"port"`
	Hosts        []string      `koanf:"hosts"`
	Server       serverBlock   `koanf:"server"`
	Routes       []routeConfig `koanf:"routes"`
	ClientSecret string        `koanf:"client_secret" secret:"true"`
	Hook         func()        `koanf:"-"`
}

type boundApp struct {
	Greeting string `koanf:"greeting"`
}

const describeGolden = `entries:
  - key: modules.http.server.default.port
    type: uint16
    default: "8080"
    env: LAKTA_MODULES__HTTP__SERVER__DEFAULT__PORT
    description: ""
    secret: false
  - key: modules.http.server.default.hosts
    type: '[]string'
    default: '["a","b"]'
    env: LAKTA_MODULES__HTTP__SERVER__DEFAULT__HOSTS
    description: ""
    secret: false
  - key: modules.http.server.default.server.read_timeout
    type: time.Duration
    default: 5s
    env: LAKTA_MODULES__HTTP__SERVER__DEFAULT__SERVER__READ_TIMEOUT
    description: ""
    secret: false
  - key: modules.http.server.default.routes
    type: '[]reflectcfg_test.routeConfig'
    default: ""
    env: LAKTA_MODULES__HTTP__SERVER__DEFAULT__ROUTES
    description: ""
    secret: false
  - key: modules.http.server.default.client_secret
    type: string
    default: <redacted>
    env: LAKTA_MODULES__HTTP__SERVER__DEFAULT__CLIENT_SECRET
    description: ""
    secret: true
  - key: app.svc.greeting
    type: string
    default: hi
    env: LAKTA_APP__SVC__GREETING
    description: ""
    secret: false
`

func TestDescribe_ExactPaths(t *testing.T) {
	t.Parallel()

	entries := []lakta.DescribedConfig{
		{Path: "modules.http.server.default", Value: describedServer{
			Port:         8080,
			Hosts:        []string{"a", "b"},
			Server:       serverBlock{ReadTimeout: 5 * time.Second},
			ClientSecret: "s3cret",
		}},
		{Path: "app.svc", Value: &boundApp{Greeting: "hi"}},
	}

	var buf bytes.Buffer
	testza.AssertNil(t, reflectcfg.Describe(&buf, entries))
	testza.AssertEqual(t, describeGolden, buf.String())
	testza.AssertNotContains(t, buf.String(), "s3cret")
}

func TestDescribe_EmptyStructHasNoEntries(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	testza.AssertNil(t, reflectcfg.Describe(&buf, []lakta.DescribedConfig{{Path: "app.svc", Value: &struct{}{}}}))
	testza.AssertEqual(t, "entries: []\n", buf.String())
}
