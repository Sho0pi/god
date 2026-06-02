package whatsapp

import (
	"testing"

	"github.com/sho0pi/god/config"
)

func TestPhoneMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0501234567", "972501234567", true},   // local IL vs full international
		{"972501234567", "972501234567", true}, // exact
		{"501234567", "972501234567", true},    // no trunk zero, missing country code
		{"0501234567", "0507654321", false},    // different numbers
		{"0501234567", "0541234567", false},    // share tail but different operator prefix
		{"1234", "9721234", false},             // core too short, reject
		{"", "972501234567", false},            // empty
	}
	for _, c := range cases {
		if got := phoneMatch(c.a, c.b); got != c.want {
			t.Errorf("phoneMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestIsAllowedMergesStoreSource(t *testing.T) {
	cfg := &config.Config{}
	cfg.Connectors.WhatsApp.Allow = nil // empty yaml list

	c := &Connector{configFn: func() *config.Config { return cfg }}
	// Empty list with no source: accept everyone.
	if !c.isAllowed("972501234567") {
		t.Fatal("empty allow list should accept all senders")
	}

	// Store-backed entry (local format) should gate, and match the
	// international sender via phoneMatch.
	c.allowSource = func() []string { return []string{"0501234567"} }
	if !c.isAllowed("972501234567") {
		t.Error("store-backed local number should match international sender")
	}
	if c.isAllowed("972507654321") {
		t.Error("non-listed sender should be blocked once list is non-empty")
	}
}
