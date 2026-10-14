package errors

import "strings"

// classPrefix marks an oops tag that selects the transport class of an error.
const classPrefix = "lakta.class:"

// Class tags select the transport status of an oops error:
//
//	oops.Code("NOTE_NOT_FOUND").Tags(errors.ClassNotFound).Public("note not found")
//
// An oops error without a class tag renders as INTERNAL unless its Code() is
// itself a canonical Code.
const (
	ClassNotFound           = classPrefix + string(CodeNotFound)
	ClassInvalidArgument    = classPrefix + string(CodeInvalidArgument)
	ClassValidation         = classPrefix + string(CodeValidation)
	ClassUnauthenticated    = classPrefix + string(CodeUnauthenticated)
	ClassPermissionDenied   = classPrefix + string(CodePermissionDenied)
	ClassAlreadyExists      = classPrefix + string(CodeAlreadyExists)
	ClassFailedPrecondition = classPrefix + string(CodeFailedPrecondition)
	ClassUnavailable        = classPrefix + string(CodeUnavailable)
	ClassInternal           = classPrefix + string(CodeInternal)
	ClassDeadlineExceeded   = classPrefix + string(CodeDeadlineExceeded)
	ClassResourceExhausted  = classPrefix + string(CodeResourceExhausted)
	ClassUnimplemented      = classPrefix + string(CodeUnimplemented)
	ClassAborted            = classPrefix + string(CodeAborted)
)

func classFromTags(tags []string) (Code, bool) {
	for _, tag := range tags {
		name, ok := strings.CutPrefix(tag, classPrefix)
		if !ok {
			continue
		}
		if _, _, known := lookup(Code(name)); known {
			return Code(name), true
		}
	}
	return "", false
}
