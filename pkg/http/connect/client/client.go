package connectclient

import (
	"context"
	"crypto/tls"
	stderrors "errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	"github.com/samber/oops"
	"golang.org/x/oauth2"
)

// Client is a configured outbound Connect client; pass HTTPClient, BaseURL
// and Options to a generated NewXxxClient constructor.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	maxDeadline time.Duration
	tokens      oauth2.TokenSource
	options     []connect.ClientOption
}

// HTTPClient returns the HTTP/2 client (mTLS when a TLS provider is set).
func (c *Client) HTTPClient() connect.HTTPClient { return c.httpClient } //nolint:ireturn // generated clients take connect.HTTPClient

// BaseURL returns the downstream base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// Options returns the client options: credentials, propagation, deadline and
// error mapping, plus OTel tracing and per-procedure metrics.
func (c *Client) Options() []connect.ClientOption { return c.options }

func newClient(tlsConfig *tls.Config, cfg Config, tokens oauth2.TokenSource) *Client {
	transport := &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true}
	c := &Client{
		httpClient:  &http.Client{Transport: transport},
		baseURL:     cfg.BaseURL,
		maxDeadline: cfg.MaxDeadline,
		tokens:      tokens,
	}

	interceptors := []connect.Interceptor{c.outbound()}
	if oi, err := otelconnect.NewInterceptor(); err == nil {
		interceptors = append([]connect.Interceptor{oi}, interceptors...)
	}
	c.options = []connect.ClientOption{connect.WithInterceptors(interceptors...)}

	return c
}

// downstreamError keeps the downstream connect error as the cause of an
// opaque error, so it is logged but never rendered to our caller.
type downstreamError struct{ cause *connect.Error }

func (e *downstreamError) Error() string { return "downstream call failed: " + e.cause.Error() }
func (e *downstreamError) Unwrap() error { return e.cause }

// DownstreamCode returns the code the downstream service answered with, or
// connect.CodeUnknown when err did not come from a downstream response.
func DownstreamCode(err error) connect.Code {
	var de *downstreamError
	if stderrors.As(err, &de) {
		return de.cause.Code()
	}
	return connect.CodeUnknown
}

func (c *Client) outbound() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the composition unit
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			token, err := c.bearer(ctx)
			if err != nil {
				return nil, oops.Tags(apperrors.ClassInternal).Wrap(err)
			}
			req.Header().Set("Authorization", "Bearer "+token)

			if id := connectmod.RequestID(ctx); id != "" {
				req.Header().Set(connectmod.RequestIDHeader, id)
			}
			if addr, ok := connectmod.ClientIP(ctx); ok {
				req.Header().Set(connectmod.ClientIPHeader, addr.String())
			}

			ctx, cancel := c.capDeadline(ctx)
			defer cancel()

			resp, err := next(ctx, req)
			if err != nil {
				return nil, mapError(err)
			}
			return resp, nil
		}
	})
}

func (c *Client) capDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.maxDeadline <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= c.maxDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.maxDeadline)
}

// mapError turns a downstream failure into UNAVAILABLE (transient) or
// INTERNAL; the downstream code and message stay in the cause.
func mapError(err error) error {
	var cerr *connect.Error
	if !stderrors.As(err, &cerr) {
		return oops.Tags(apperrors.ClassUnavailable).Wrap(err)
	}

	class := apperrors.ClassInternal
	switch cerr.Code() { //nolint:exhaustive // everything else is not worth retrying
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeResourceExhausted, connect.CodeAborted:
		class = apperrors.ClassUnavailable
	}
	return oops.Code("DOWNSTREAM_FAILED").Tags(class).Wrap(&downstreamError{cause: cerr})
}
