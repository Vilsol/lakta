package connectclient

import (
	"context"

	"github.com/Vilsol/lakta/pkg/auth/verifier"
	"github.com/samber/oops"
)

type credentialMode int

const (
	modeUser credentialMode = iota + 1
	modeService
	modeToken
)

type (
	modeKey  struct{}
	tokenKey struct{}
)

// AsUser makes calls on ctx forward the inbound user's verified token.
func AsUser(ctx context.Context) context.Context {
	return context.WithValue(ctx, modeKey{}, modeUser)
}

// AsService makes calls on ctx use this service's client-credentials token.
func AsService(ctx context.Context) context.Context {
	return context.WithValue(ctx, modeKey{}, modeService)
}

// WithToken makes calls on ctx use raw as the bearer token.
func WithToken(ctx context.Context, raw string) context.Context {
	return context.WithValue(context.WithValue(ctx, modeKey{}, modeToken), tokenKey{}, raw)
}

func (c *Client) bearer(ctx context.Context) (string, error) {
	mode, _ := ctx.Value(modeKey{}).(credentialMode)
	switch mode {
	case modeUser:
		p, ok := verifier.PrincipalFrom(ctx)
		if !ok || p.Raw == "" {
			return "", oops.Errorf("connectclient: AsUser call without an inbound user token")
		}
		return p.Raw.Reveal(), nil
	case modeService:
		if c.tokens == nil {
			return "", oops.Errorf("connectclient: AsService call but client credentials are not configured")
		}
		tok, err := c.tokens.Token()
		if err != nil {
			return "", oops.Wrapf(err, "connectclient: fetch service token")
		}
		return tok.AccessToken, nil
	case modeToken:
		raw, _ := ctx.Value(tokenKey{}).(string)
		if raw == "" {
			return "", oops.Errorf("connectclient: WithToken call with an empty token")
		}
		return raw, nil
	default:
		return "", oops.Errorf("connectclient: no credentials chosen; use AsUser, AsService or WithToken")
	}
}
