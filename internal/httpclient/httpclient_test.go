package httpclient

import (
	"testing"
	"time"
)

func TestClientTimeout(t *testing.T) {
	want := 30 * time.Second
	if Client.Timeout != want {
		t.Errorf("Client.Timeout = %v, want %v", Client.Timeout, want)
	}
}

func TestUserAgent(t *testing.T) {
	want := "rdr/1.0"
	if UserAgent != want {
		t.Errorf("UserAgent = %q, want %q", UserAgent, want)
	}
}

func TestMaxResponseSize(t *testing.T) {
	want := 10 << 20
	if MaxResponseSize != want {
		t.Errorf("MaxResponseSize = %d, want %d", MaxResponseSize, want)
	}
}
