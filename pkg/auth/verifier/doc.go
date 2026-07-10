// Package verifier provides multi-issuer JWT verification for lakta services.
//
// A Registry holds one JWKS-backed verifier per configured issuer (keyed by the
// exact iss claim) plus an isolated HS256 static-key dev path, and exposes a
// single Verify entry point the fiber and grpc auth adapters call. Every
// verification failure surfaces as an opaque errors.Unauthenticated; the package
// never reveals which check failed.
package verifier
