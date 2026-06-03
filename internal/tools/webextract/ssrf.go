package webextract

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// isBlockedIP reports whether dialing ip would reach a non-public address. This
// is the SSRF boundary: the URL the model passes is untrusted (it can originate
// from web_search results or message text), so a prompt-injection must not be
// able to make god fetch internal services or the cloud metadata endpoint.
//
// Link-local (169.254.0.0/16, fe80::/10) covers the cloud metadata IP
// 169.254.169.254; IsPrivate covers RFC1918 and fc00::/7.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// Carrier-grade NAT 100.64.0.0/10 — not covered by IsPrivate.
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// safeControl is a net.Dialer.Control hook. It runs after DNS resolution with
// the concrete ip:port about to be dialed, so it catches hostnames that resolve
// to internal IPs (DNS-rebinding included) at connect time.
func safeControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf guard: bad address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("ssrf guard: unresolved host %q", host)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("ssrf guard: blocked non-public address %s", ip)
	}
	return nil
}

// validateURL checks scheme and embedded-credential rules before any network
// access. Only http(s) is allowed; URLs with userinfo (which can smuggle
// tokens/keys) are rejected, mirroring the Hermes extractor. IP-range blocking
// is config-gated and handled separately (see blockedLiteralIP and the dialer
// Control hook), so this stays guard-independent.
func validateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("unsupported scheme %q (only http/https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("url has no host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("url must not contain embedded credentials")
	}
	return u, nil
}

// blockedLiteralIP reports whether the URL's host is a bare IP literal in a
// non-public range. Used only when the SSRF guard is enabled, to fail early
// with a clear error before dialing; the dialer Control hook is the real
// enforcement (it also catches hostnames that resolve to internal IPs).
func blockedLiteralIP(u *url.URL) bool {
	ip := net.ParseIP(u.Hostname())
	return ip != nil && isBlockedIP(ip)
}
