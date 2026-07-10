package fiber_test

import (
	stderrors "errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	pkgerrors "github.com/Vilsol/lakta/pkg/errors"
	valfiber "github.com/Vilsol/lakta/pkg/validation/fiber"
	"github.com/go-playground/validator/v10"
)

const validEmail = "a@b.com"

type address struct {
	Zip string `json:"zip" validate:"required"`
}

type user struct {
	Email   string  `json:"email"   validate:"required,email"`
	Address address `json:"address"`
}

type item struct {
	Qty int `json:"qty" validate:"gt=0"`
}

type signup struct {
	User  user   `json:"user"`
	Items []item `json:"items" validate:"dive"`
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var appErr *pkgerrors.AppError
	testza.AssertTrue(t, stderrors.As(err, &appErr), "expected *AppError")
	testza.AssertEqual(t, pkgerrors.CodeValidation, appErr.Code)
	out := make(map[string]string, len(appErr.Fields))
	for _, f := range appErr.Fields {
		out[f.Field] = f.Description
	}
	return out
}

func TestValidateSuccess(t *testing.T) {
	t.Parallel()
	v := valfiber.New()
	err := v.Validate(&signup{
		User:  user{Email: validEmail, Address: address{Zip: "12345"}},
		Items: []item{{Qty: 1}},
	})
	testza.AssertNoError(t, err)
}

func TestValidateRequiredEmail(t *testing.T) {
	t.Parallel()
	v := valfiber.New()
	err := v.Validate(&signup{
		User:  user{Email: "not-an-email", Address: address{Zip: "12345"}},
		Items: []item{{Qty: 1}},
	})
	fields := fieldsOf(t, err)
	testza.AssertEqual(t, "email", fields["user.email"])
}

func TestValidateNested(t *testing.T) {
	t.Parallel()
	v := valfiber.New()
	err := v.Validate(&signup{
		User:  user{Email: validEmail, Address: address{Zip: ""}},
		Items: []item{{Qty: 1}},
	})
	fields := fieldsOf(t, err)
	testza.AssertEqual(t, "required", fields["user.address.zip"])
}

func TestValidateSliceElement(t *testing.T) {
	t.Parallel()
	v := valfiber.New()
	err := v.Validate(&signup{
		User:  user{Email: validEmail, Address: address{Zip: "12345"}},
		Items: []item{{Qty: 0}},
	})
	fields := fieldsOf(t, err)
	testza.AssertEqual(t, "gt", fields["items[0].qty"])
}

func TestValidateNonStructTargetIsInternal(t *testing.T) {
	t.Parallel()
	v := valfiber.New()
	err := v.Validate(42)
	var appErr *pkgerrors.AppError
	testza.AssertTrue(t, stderrors.As(err, &appErr))
	testza.AssertEqual(t, pkgerrors.CodeInternal, appErr.Code)
}

func TestValidateWithCustomValidate(t *testing.T) {
	t.Parallel()
	custom := validator.New()
	v := valfiber.New(valfiber.WithValidate(custom))
	err := v.Validate(&signup{
		User:  user{Email: "bad", Address: address{Zip: "12345"}},
		Items: []item{{Qty: 1}},
	})
	fields := fieldsOf(t, err)
	testza.AssertEqual(t, "email", fields["user.email"])
}
