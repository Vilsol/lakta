// Package fiber renders lakta AppErrors as RFC 9457 application/problem+json.
// Import it aliased (e.g. errfiber) to avoid clashing with gofiber/fiber.
package fiber

import (
	stderrors "errors"
	"net/http"

	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/gofiber/fiber/v3"
)

const (
	// problemContentType is the RFC 9457 media type; always emitted, never text/html.
	problemContentType = "application/problem+json"
	// typeURNPrefix builds the problem `type` URI: urn:lakta:error:{code}.
	typeURNPrefix = "urn:lakta:error:"
	// internalDetail is the opaque detail rendered for every INTERNAL error.
	internalDetail = "internal error"
)

// problemDetail is the RFC 9457 application/problem+json body. `type` is the URN
// urn:lakta:error:{code}; invalid_params is derived from AppError.Fields.
type problemDetail struct {
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail"`
	Code          errpkg.Code    `json:"code"`
	InvalidParams []invalidParam `json:"invalid_params,omitempty"`
}

type invalidParam struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// config holds ErrorHandler options; titleFunc maps an HTTP status to its title.
type config struct {
	titleFunc func(int) string
}

// Option configures the ErrorHandler.
type Option func(*config)

// ErrorHandler returns a fiber.ErrorHandler that renders every handler error as
// application/problem+json. It converts via errors.FromError, then additionally
// maps a raw *fiber.Error (framework 404/405/…) into the equivalent AppError so
// framework and app errors render identically. Content-Type is ALWAYS
// application/problem+json (no text/html fallback); the request body is never
// reflected into detail. An INTERNAL error renders detail "internal error" only.
func ErrorHandler(opts ...Option) fiber.ErrorHandler {
	cfg := &config{titleFunc: http.StatusText}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c fiber.Ctx, err error) error {
		appErr := toAppError(err)

		detail := appErr.Message
		if appErr.Code == errpkg.CodeInternal {
			detail = internalDetail
		}

		body := problemDetail{
			Type:   typeURN(appErr.Code),
			Title:  cfg.titleFunc(appErr.HTTP),
			Status: appErr.HTTP,
			Detail: detail,
			Code:   appErr.Code,
		}
		for _, f := range appErr.Fields {
			body.InvalidParams = append(body.InvalidParams, invalidParam{Name: f.Field, Reason: f.Description})
		}

		return c.Status(appErr.HTTP).JSON(body, problemContentType)
	}
}

// toAppError normalizes a handler error, mapping a raw *fiber.Error before
// falling back to errors.FromError so framework errors render consistently.
func toAppError(err error) *errpkg.AppError {
	var fe *fiber.Error
	if stderrors.As(err, &fe) {
		return fromFiberError(fe)
	}
	return errpkg.FromError(err)
}

// typeURN formats the URN: "urn:lakta:error:" + string(code).
func typeURN(code errpkg.Code) string {
	return typeURNPrefix + string(code)
}

// fromFiberError maps a *fiber.Error's StatusCode to the matching AppError
// (404→NotFound, 405→FailedPrecondition, else Internal).
func fromFiberError(fe *fiber.Error) *errpkg.AppError {
	switch fe.Code {
	case http.StatusNotFound:
		return errpkg.NotFound(fe.Message)
	case http.StatusMethodNotAllowed:
		return errpkg.FailedPrecondition(fe.Message)
	default:
		return errpkg.Internal(internalDetail)
	}
}
