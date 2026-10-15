// Package connectclient provides an outbound Connect client [Module] for
// service-to-service calls: SPIFFE mTLS (via a TLS provider such as
// spiffe.ClientTLS(serverID)), explicit per-call credentials, propagation of
// request ID, trace context and the verified client IP, a capped deadline, and
// downstream errors that never pass through to the caller.
//
// Every call must choose its credentials on ctx:
//
//	connectclient.AsUser(ctx)          // forward the inbound user's token; fails without one
//	connectclient.AsService(ctx)       // client-credentials token of this service
//	connectclient.WithToken(ctx, raw)  // an explicit token (tools, smoke tests)
//
// A call without a choice fails before dialing.
package connectclient
