package errors

import "fmt"

// stringifyCode renders an oops Code() (any) as a string; an empty/nil code
// yields "" so FromError falls back to CodeInternal.
func stringifyCode(code any) string {
	if code == nil {
		return ""
	}
	if s, ok := code.(string); ok {
		return s
	}
	return fmt.Sprint(code)
}

// stringifyValue renders an oops Context() value (any) as a string for Meta.
func stringifyValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
