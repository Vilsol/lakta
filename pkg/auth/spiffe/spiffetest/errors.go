package spiffetest

import "errors"

var (
	errNoCertificate = errors.New("spiffetest: peer presented no certificate")
	errUnexpectedID  = errors.New("spiffetest: peer SPIFFE ID mismatch")
)
