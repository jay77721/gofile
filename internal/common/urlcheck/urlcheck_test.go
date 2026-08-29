package urlcheck

import "testing"

func TestValidatePublicURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		private bool // allowPrivate
		wantErr bool
	}{
		// Note: public cases use IP literals to avoid DNS timeout in offline environments
		{"public https ok", "https://8.8.8.8/v1", false, false},
		{"public http ok", "http://8.8.8.8", false, false},
		{"public with port ok", "https://8.8.8.8:8443/v1/", false, false},
		{"loopback rejected", "http://127.0.0.1:11434", false, true},
		{"localhost rejected", "http://localhost:11434", false, true},
		{"rfc1918 rejected", "http://10.0.0.5:8000", false, true},
		{"rfc1918-172 rejected", "http://172.16.0.1:8000", false, true},
		{"rfc1918-192 rejected", "http://192.168.1.100:8080", false, true},
		{"linklocal rejected", "http://169.254.169.254/latest/meta-data", false, true},
		{"ipv6 loopback rejected", "http://[::1]:11434", false, true},
		{"ipv6 ula rejected", "http://[fc00::1]:11434", false, true},
		{"ftp scheme rejected", "ftp://example.com", false, true},
		{"no scheme rejected", "example.com/v1", false, true},
		{"empty host rejected", "https:///v1", false, true},
		{"private allowed when flag on", "http://localhost:11434", true, false},
		{"private allowed when flag on 2", "http://192.168.1.10:8080", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePublicURL(c.raw, c.private)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePublicURL(%q, %v) err = %v, wantErr %v", c.raw, c.private, err, c.wantErr)
			}
		})
	}
}
