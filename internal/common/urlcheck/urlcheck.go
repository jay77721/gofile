package urlcheck

import (
	"fmt"
	"net"
	"net/url"
)

// ValidatePublicURL validates a custom LLM endpoint URL:
//   - only http/https are allowed
//   - private networks are rejected by default (SSRF protection); allowed when allowPrivate=true (for local Ollama/vLLM deployments)
//
// Note: this does DNS resolution validation; extreme DNS rebinding cases need hardening at the connection layer, out of scope for file service.
func ValidatePublicURL(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	if allowPrivate {
		return nil
	}

	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS resolution failed: if host itself is an IP literal use it directly, otherwise error
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			return fmt.Errorf("resolve host failed: %w", err)
		}
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private network address not allowed: %s", ip)
		}
	}
	return nil
}

// isPrivateIP checks whether an IP belongs to private/reserved ranges.
// Covers: loopback (127/8, ::1), RFC1918 (10/8, 172.16/12, 192.168/16),
// link-local (169.254/16, fe80::/10), IPv6 ULA (fc00::/7), unspecified address.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 fallback (IsLinkLocalUnicast should also cover IPv4, double-checked here)
		return ip4[0] == 169 && ip4[1] == 254
	}
	return false
}
