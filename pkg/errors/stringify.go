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
