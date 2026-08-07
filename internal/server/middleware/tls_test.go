package middleware

import "crypto/tls"

// tlsConnectionState stands in for a completed handshake in tests that need
// http.Request.TLS to be non-nil.
var tlsConnectionState = tls.ConnectionState{
	Version:           tls.VersionTLS13,
	HandshakeComplete: true,
}
