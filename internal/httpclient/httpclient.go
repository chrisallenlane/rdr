// Package httpclient provides a shared HTTP client and constants for
// outbound requests made by the poller and feed discovery subsystems.
package httpclient

import (
	"net/http"
	"time"
)

// UserAgent is the User-Agent header value sent on all outbound requests.
const UserAgent = "rdr/1.0"

// MaxResponseSize is the maximum response body size (10 MB) accepted when
// fetching feeds or performing feed discovery.
const MaxResponseSize = 10 << 20

// Client is a shared HTTP client with a 30-second timeout.
var Client = &http.Client{
	Timeout: 30 * time.Second,
}
