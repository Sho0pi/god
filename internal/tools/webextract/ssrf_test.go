package webextract

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},           // loopback
		{"::1", true},                 // loopback v6
		{"10.0.0.5", true},            // RFC1918
		{"192.168.1.1", true},         // RFC1918
		{"172.16.0.1", true},          // RFC1918
		{"169.254.169.254", true},     // link-local / cloud metadata
		{"fe80::1", true},             // link-local v6
		{"0.0.0.0", true},             // unspecified
		{"100.64.0.1", true},          // CGNAT
		{"fc00::1", true},             // unique-local v6
		{"8.8.8.8", false},            // public
		{"1.1.1.1", false},            // public
		{"93.184.216.34", false},      // public (example.com)
		{"2606:2800:220:1::1", false}, // public v6
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", c.ip)
		}
		if got := isBlockedIP(ip); got != c.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

func TestValidateURL(t *testing.T) {
	bad := []string{
		"ftp://example.com",            // scheme
		"file:///etc/passwd",           // scheme
		"http://user:pass@example.com", // embedded creds
		"not a url",                    // no host
		"http://",                      // no host
	}
	for _, u := range bad {
		if _, err := validateURL(u); err == nil {
			t.Errorf("validateURL(%q) = nil error, want rejection", u)
		}
	}

	good := []string{
		"http://example.com",
		"https://example.com/path?q=1",
		"https://sub.domain.test:8443/x",
		"http://127.0.0.1/admin", // valid url; IP blocking is gated separately
	}
	for _, u := range good {
		if _, err := validateURL(u); err != nil {
			t.Errorf("validateURL(%q) = %v, want ok", u, err)
		}
	}
}

func TestBlockedLiteralIP(t *testing.T) {
	blocked := []string{"http://127.0.0.1/admin", "http://169.254.169.254/", "http://10.1.2.3/"}
	for _, raw := range blocked {
		u, err := validateURL(raw)
		if err != nil {
			t.Fatalf("validateURL(%q): %v", raw, err)
		}
		if !blockedLiteralIP(u) {
			t.Errorf("blockedLiteralIP(%q) = false, want true", raw)
		}
	}
	u, _ := validateURL("http://example.com")
	if blockedLiteralIP(u) {
		t.Error("blockedLiteralIP(example.com) = true, want false (hostname, not IP)")
	}
}
