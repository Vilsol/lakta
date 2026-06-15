package grpcclient_test

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	grpcclient "github.com/Vilsol/lakta/pkg/grpc/client"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGetCredentials_DefaultNil(t *testing.T) {
	t.Parallel()

	c := grpcclient.NewConfig()
	creds, err := c.GetCredentials()
	testza.AssertNil(t, err)
	testza.AssertNil(t, creds)
}

func TestGetCredentials_Insecure(t *testing.T) {
	t.Parallel()

	c := grpcclient.NewConfig(grpcclient.WithInsecure(true))
	creds, err := c.GetCredentials()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, creds)
}

func TestGetCredentials_ExplicitWin(t *testing.T) {
	t.Parallel()

	explicit := insecure.NewCredentials()
	c := grpcclient.NewConfig(grpcclient.WithCredentials(explicit))
	creds, err := c.GetCredentials()
	testza.AssertNil(t, err)
	testza.AssertEqual(t, explicit, creds)
}

func TestDialOptions_DefaultNoTransportCreds(t *testing.T) {
	t.Parallel()

	// Default config has no transport credentials; DialOptions returns only the
	// stats handler and keepalive options.
	c := grpcclient.NewConfig()
	opts, err := c.DialOptions()
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 2, len(opts))
}

func TestDialOptions_InsecureAddsTransportCreds(t *testing.T) {
	t.Parallel()

	c := grpcclient.NewConfig(grpcclient.WithInsecure(true))
	opts, err := c.DialOptions()
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 3, len(opts))
}

func TestDialOptions_BadTLSCAFileErrors(t *testing.T) {
	t.Parallel()

	c := grpcclient.NewConfig()
	c.TLS = config.TLS{CAFile: "/nonexistent/ca.pem"}

	opts, err := c.DialOptions()
	testza.AssertNotNil(t, err)
	testza.AssertNil(t, opts)
}
