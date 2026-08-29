package cryptoutil

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	secret := "test-secret-key"
	plain := "sk-proj-abcdef1234567890"

	enc, err := EncryptSecret(secret, plain)
	if err != nil {
		t.Fatalf("EncryptSecret failed: %v", err)
	}
	if enc == "" || enc == plain {
		t.Fatalf("ciphertext should differ from plaintext")
	}

	dec, err := DecryptSecret(secret, enc)
	if err != nil {
		t.Fatalf("DecryptSecret failed: %v", err)
	}
	if dec != plain {
		t.Fatalf("round trip mismatch: got %q want %q", dec, plain)
	}
}

func TestEncryptDecryptEmpty(t *testing.T) {
	enc, err := EncryptSecret("k", "")
	if err != nil || enc != "" {
		t.Fatalf("empty plaintext should produce empty ciphertext, got %q err %v", enc, err)
	}
	dec, err := DecryptSecret("k", "")
	if err != nil || dec != "" {
		t.Fatalf("empty ciphertext should produce empty plaintext, got %q err %v", dec, err)
	}
}

func TestDecryptWrongSecretFails(t *testing.T) {
	enc, err := EncryptSecret("key-a", "sk-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptSecret("key-b", enc); err == nil {
		t.Fatal("decrypt with wrong secret should fail")
	}
}

func TestEncryptRandomizedNonce(t *testing.T) {
	// 相同明文两次加密应产生不同密文(随机 nonce)
	a, _ := EncryptSecret("k", "sk-abcdef")
	b, _ := EncryptSecret("k", "sk-abcdef")
	if a == b {
		t.Fatal("two encryptions of same plaintext should differ (random nonce)")
	}
}

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sk-proj-abcdef123456", "sk-p****3456"},
		{"short", "****"},
		{"12345678", "****"},
		{"", "****"},
	}
	for _, c := range cases {
		if got := MaskSecret(c.in); got != c.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaskSecretNoPlaintextLeak(t *testing.T) {
	masked := MaskSecret("sk-proj-very-secret-key-9876543210")
	for _, part := range strings.Split(masked, "****") {
		if part == "" {
			continue
		}
		// 掩码保留的是首尾,不应出现中间片段
		if strings.Contains(masked, "very-secret") {
			t.Fatalf("masked secret leaks middle content: %s", masked)
		}
	}
}
