// Package fiber adapts go-playground/validator/v10 into a fiber v3
// StructValidator that fails as a lakta *errors.AppError{Code: VALIDATION}.
// Import it aliased (e.g. valfiber) to avoid clashing with gofiber/fiber.
//
// ctx.Bind().Body(&dto) auto-invokes Validate; a struct-tag failure becomes an
// AppError the Phase 5 fiber ErrorHandler renders as application/problem+json
// with invalid_params. Field paths are normalized to json-tag dotted/bracketed
// form (user.email, items[0].qty) so they match the grpc transport byte-for-byte.
package fiber

import (
	stderrors "errors"
	"reflect"
	"strings"

	pkgerrors "github.com/Vilsol/lakta/pkg/errors"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
)

// defaultTagName is validator/v10's rule tag ("validate:...").
const defaultTagName = "validate"

// Option configures the StructValidator.
type Option func(*StructValidator)

// StructValidator satisfies fiber v3's fiber.StructValidator. A failure becomes
// an *errors.AppError{Code: VALIDATION} carrying one FieldViolation per failed
// field.
type StructValidator struct {
	validate *validator.Validate // default instance unless WithValidate overrides
	tagName  string              // rule tag driving validation; default "validate"
}

// New builds a StructValidator. With no options it uses a default validator.New();
// options can supply a pre-built *validator.Validate (custom validations) or change
// the rule tag. The json-tag field-name func is always registered so paths stay
// consistent with the grpc transport.
func New(opts ...Option) fiber.StructValidator { //nolint:ireturn // fiber.Config requires the fiber.StructValidator interface
	s := &StructValidator{tagName: defaultTagName}
	for _, opt := range opts {
		opt(s)
	}
	if s.validate == nil {
		s.validate = validator.New()
	}
	s.validate.SetTagName(s.tagName)
	s.validate.RegisterTagNameFunc(jsonFieldName)
	return s
}

// WithValidate supplies a caller-built *validator.Validate (registered custom
// validators, aliases, etc.).
func WithValidate(v *validator.Validate) Option {
	return func(s *StructValidator) { s.validate = v }
}

// WithTagName overrides the rule-tag key (default "validate").
func WithTagName(name string) Option {
	return func(s *StructValidator) { s.tagName = name }
}

// Validate runs the validator over out. It returns nil on success; on
// validator.ValidationErrors it returns errors.Validation("validation failed")
// with one WithField per failed field. Any other error (e.g. an
// InvalidValidationError from a non-struct target) is wrapped as errors.Internal.
func (s *StructValidator) Validate(out any) error {
	err := s.validate.Struct(out)
	if err == nil {
		return nil
	}

	var ve validator.ValidationErrors
	if stderrors.As(err, &ve) {
		appErr := pkgerrors.Validation("validation failed")
		for _, fe := range ve {
			appErr = appErr.WithField(fieldName(fe), fe.Tag())
		}
		return appErr
	}

	return pkgerrors.Internal("invalid validation target").WithCause(err)
}

// jsonFieldName drives validator's alternate field names off the json tag so the
// rendered paths are json-style (user.email) rather than Go field names.
func jsonFieldName(fld reflect.StructField) string {
	name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
	if name == "-" {
		return ""
	}
	if name == "" {
		return fld.Name
	}
	return name
}

// fieldName derives the dotted/bracketed path from fe.Namespace(), stripping the
// root struct name so nested and slice-element fields emit user.email / items[0].qty.
func fieldName(fe validator.FieldError) string {
	ns := fe.Namespace()
	if _, after, ok := strings.Cut(ns, "."); ok {
		return after
	}
	return fe.Field()
}
