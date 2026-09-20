package security

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestGenerateTokenContainsExactly32RandomBytes(t *testing.T) {
	first, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("GenerateToken returned the same token twice")
	}
	for name, token := range map[string]string{"first": first, "second": second} {
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("%s token is not valid raw URL base64: %v", name, err)
		}
		if len(raw) != TokenEntropyBytes {
			t.Fatalf("%s token decoded length = %d, want %d", name, len(raw), TokenEntropyBytes)
		}
	}
}

func TestTokenDigestUsesPepperAndVerifiesConstantTime(t *testing.T) {
	pepper := []byte("server-side pepper")
	token := "registration-secret"
	digest := Digest(pepper, token)

	if len(digest) != 32 {
		t.Fatalf("digest length = %d, want SHA-256 length", len(digest))
	}
	if !Verify(pepper, token, digest) {
		t.Fatal("Verify rejected the original token")
	}
	if Verify(pepper, "wrong", digest) {
		t.Fatal("Verify accepted a different token")
	}
	if Verify([]byte("different pepper"), token, digest) {
		t.Fatal("Verify accepted a different pepper")
	}
	if Verify(pepper, token, append([]byte(nil), digest[:31]...)) {
		t.Fatal("Verify accepted a truncated digest")
	}
	if bytes.Equal(digest, Digest(nil, token)) {
		t.Fatal("pepper did not change the digest")
	}
}
