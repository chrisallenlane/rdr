package httpclient

import (
	"testing"
)

func TestClientTimeout(t *testing.T) {
	if Client.Timeout <= 0 {
		t.Errorf("Client.Timeout = %v, want > 0", Client.Timeout)
	}
}

func TestUserAgent(t *testing.T) {
	if UserAgent == "" {
		t.Error("UserAgent is empty, want non-empty string")
	}
}

func TestMaxResponseSize(t *testing.T) {
	if MaxResponseSize <= 0 {
		t.Errorf("MaxResponseSize = %d, want > 0", MaxResponseSize)
	}
}
