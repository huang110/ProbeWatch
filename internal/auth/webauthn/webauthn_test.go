package webauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestCBORCodec(t *testing.T) {
	// Test positive int
	val, n, err := DecodeCBOR([]byte{0x18, 0x64}) // 100
	if err != nil || val != int64(100) || n != 2 {
		t.Fatalf("decode int failed: val=%v, n=%d, err=%v", val, n, err)
	}

	// Test negative int
	val, n, err = DecodeCBOR([]byte{0x26}) // -1 - 6 = -7
	if err != nil || val != int64(-7) || n != 1 {
		t.Fatalf("decode neg int failed: val=%v, n=%d, err=%v", val, n, err)
	}

	// Test byte string
	val, n, err = DecodeCBOR([]byte{0x44, 't', 'e', 's', 't'})
	if err != nil || string(val.([]byte)) != "test" || n != 5 {
		t.Fatalf("decode bytes failed: val=%v, n=%d, err=%v", val, n, err)
	}

	// Test text string
	val, n, err = DecodeCBOR([]byte{0x65, 'h', 'e', 'l', 'l', 'o'})
	if err != nil || val.(string) != "hello" || n != 6 {
		t.Fatalf("decode string failed: val=%v, n=%d, err=%v", val, n, err)
	}
}

func TestWebAuthnRegistrationAndAssertion(t *testing.T) {
	rpID := "tz.115yu.us.ci"
	origin := "https://tz.115yu.us.ci"

	// 1. Generate challenge
	rawChallenge, challengeB64, err := GenerateChallenge()
	if err != nil {
		t.Fatalf("generate challenge: %v", err)
	}

	// 2. Generate test EC key pair (P-256)
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	pub := privKey.PublicKey

	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	// Pad to 32 bytes if necessary
	pad32 := func(b []byte) []byte {
		if len(b) >= 32 {
			return b
		}
		res := make([]byte, 32)
		copy(res[32-len(b):], b)
		return res
	}
	xBytes = pad32(xBytes)
	yBytes = pad32(yBytes)

	// Construct COSE Key CBOR bytes manually
	// Map(5 items):
	// 1: 2 (kty: EC2) -> 0x01, 0x02
	// 3: -7 (alg: ES256) -> 0x03, 0x26
	// -1: 1 (crv: P-256) -> 0x20, 0x01
	// -2: x (32 bytes) -> 0x21, 0x58, 0x20, x...
	// -3: y (32 bytes) -> 0x22, 0x58, 0x20, y...
	var coseKey []byte
	coseKey = append(coseKey, 0xa5)                       // map of 5 items
	coseKey = append(coseKey, 0x01, 0x02)                 // 1: 2
	coseKey = append(coseKey, 0x03, 0x26)                 // 3: -7
	coseKey = append(coseKey, 0x20, 0x01)                 // -1: 1
	coseKey = append(coseKey, 0x21, 0x58, 0x20)           // -2: bytes(32)
	coseKey = append(coseKey, xBytes...)                  // x
	coseKey = append(coseKey, 0x22, 0x58, 0x20)           // -3: bytes(32)
	coseKey = append(coseKey, yBytes...)                  // y

	// Construct authData for registration:
	// rpIdHash(32) + flags(1, UP=0x01 | AT=0x40 = 0x45) + signCount(4) + aaguid(16) + credIdLen(2) + credId(16) + coseKey
	rpIdHash := sha256.Sum256([]byte(rpID))
	credID := []byte("test-credential-id-1234")
	aaguid := make([]byte, 16)

	var authData []byte
	authData = append(authData, rpIdHash[:]...)
	authData = append(authData, 0x45) // UP(0x01) | UV(0x04) | AT(0x40)
	authData = append(authData, 0x00, 0x00, 0x00, 0x01) // signCount = 1
	authData = append(authData, aaguid...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(credID)))
	authData = append(authData, credID...)
	authData = append(authData, coseKey...)

	// Construct attestationObject (CBOR map of 2 items: "fmt": "none", "authData": authData)
	var attestationObject []byte
	attestationObject = append(attestationObject, 0xa2) // map of 2 items
	// "fmt": "none"
	attestationObject = append(attestationObject, 0x63, 'f', 'm', 't')
	attestationObject = append(attestationObject, 0x64, 'n', 'o', 'n', 'e')
	// "authData": bytes
	attestationObject = append(attestationObject, 0x68, 'a', 'u', 't', 'h', 'D', 'a', 't', 'a')
	if len(authData) <= 255 {
		attestationObject = append(attestationObject, 0x58, byte(len(authData)))
	} else {
		attestationObject = append(attestationObject, 0x59, byte(len(authData)>>8), byte(len(authData)&0xff))
	}
	attestationObject = append(attestationObject, authData...)

	// Construct clientDataJSON for registration
	clientDataReg, _ := json.Marshal(ClientData{
		Type:      "webauthn.create",
		Challenge: challengeB64,
		Origin:    origin,
	})

	// Verify Registration
	parsed, err := VerifyRegistration(credID, clientDataReg, attestationObject, rawChallenge, origin, rpID)
	if err != nil {
		t.Fatalf("VerifyRegistration failed: %v", err)
	}
	if string(parsed.CredentialID) != string(credID) {
		t.Fatalf("credID mismatch: got %v", parsed.CredentialID)
	}
	if parsed.Algorithm != -7 {
		t.Fatalf("algorithm mismatch: got %d", parsed.Algorithm)
	}
	if len(parsed.PublicKey) == 0 {
		t.Fatal("empty public key DER")
	}

	// 3. Test Assertion / Login flow
	loginChallenge, loginChallengeB64, err := GenerateChallenge()
	if err != nil {
		t.Fatalf("generate login challenge: %v", err)
	}

	clientDataLogin, _ := json.Marshal(ClientData{
		Type:      "webauthn.get",
		Challenge: loginChallengeB64,
		Origin:    origin,
	})

	// Construct authenticatorData for assertion: rpIdHash(32) + flags(0x05: UP|UV) + signCount(4, e.g. 2)
	var authDataLogin []byte
	authDataLogin = append(authDataLogin, rpIdHash[:]...)
	authDataLogin = append(authDataLogin, 0x05)
	authDataLogin = append(authDataLogin, 0x00, 0x00, 0x00, 0x02) // signCount = 2

	clientDataHash := sha256.Sum256(clientDataLogin)
	signedData := append(authDataLogin, clientDataHash[:]...)
	dataHash := sha256.Sum256(signedData)

	signature, err := ecdsa.SignASN1(rand.Reader, privKey, dataHash[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	newCount, err := VerifyAssertion(credID, clientDataLogin, authDataLogin, signature, loginChallenge, origin, rpID, parsed.PublicKey, 1)
	if err != nil {
		t.Fatalf("VerifyAssertion failed: %v", err)
	}
	if newCount != 2 {
		t.Fatalf("expected sign count 2, got %d", newCount)
	}

	// Test replay detection (count <= stored)
	_, err = VerifyAssertion(credID, clientDataLogin, authDataLogin, signature, loginChallenge, origin, rpID, parsed.PublicKey, 2)
	if err == nil {
		t.Fatal("expected replay error when sign count is not increasing")
	}

	// Test invalid signature fails
	signature[len(signature)-1] ^= 0xff
	_, err = VerifyAssertion(credID, clientDataLogin, authDataLogin, signature, loginChallenge, origin, rpID, parsed.PublicKey, 1)
	if err == nil {
		t.Fatal("expected error with tampered signature")
	}
}
