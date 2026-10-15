// Package spiffe provides an X.509-SVID source from the SPIFFE Workload API as
// a lakta [Module], plus TLS config builders for servers (accept any member of
// the trust domain) and clients (accept only an expected server ID).
//
// Init never waits for the agent: the source connects in the background and
// TLS handshakes fail until the first SVID arrives. Readiness (registered on
// *health.Health when present) is false until then and whenever the current
// SVID has expired. Rotation needs no restart.
package spiffe
