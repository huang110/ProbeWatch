package webauthn

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

var (
	ErrInvalidClientData    = errors.New("invalid clientDataJSON")
	ErrChallengeMismatch    = errors.New("challenge mismatch")
	ErrOriginMismatch       = errors.New("origin mismatch")
	ErrRPIDMismatch         = errors.New("rpIdHash mismatch")
	ErrUserNotPresent       = errors.New("user present (UP) flag not set")
	ErrSignatureInvalid     = errors.New("cryptographic signature verification failed")
	ErrUnsupportedAlgorithm = errors.New("unsupported public key algorithm")
	ErrAttestationMalformed = errors.New("malformed attestation object")
)

type ClientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

type ParsedAttestation struct {
	AAGUID       []byte
	CredentialID []byte
	PublicKey    []byte // DER PKIX encoded
	Algorithm    int64
	SignCount    uint32
}

// GenerateChallenge creates 32 cryptographically secure random bytes.
func GenerateChallenge() ([]byte, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, "", fmt.Errorf("generate challenge: %w", err)
	}
	return b, Base64URLEncode(b), nil
}

// Base64URLEncode encodes bytes into raw base64url format without padding.
func Base64URLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// Base64URLDecode decodes a base64url string with or without padding.
func Base64URLDecode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	// Try RawURLEncoding first
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	// Try URLEncoding with padding
	b, err = base64.URLEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	// Try StdEncoding fallback
	return base64.StdEncoding.DecodeString(s)
}

// ExtractRPID derives the relying party domain from a host or URL string.
func ExtractRPID(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			raw = u.Host
		}
	}
	if host, _, err := strings.Cut(raw, ":"); host != "" && err {
		return host
	}
	return raw
}

// NormalizeOrigin standardizes the origin string for comparison.
func NormalizeOrigin(origin string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	return strings.ToLower(origin)
}

// MatchOrigin verifies if client origin matches expected server origin.
func MatchOrigin(clientOrigin, expectedOrigin string) bool {
	c := NormalizeOrigin(clientOrigin)
	e := NormalizeOrigin(expectedOrigin)
	if c == e {
		return true
	}
	// Allow reverse-proxy / local port variations if host matches
	uC, errC := url.Parse(c)
	uE, errE := url.Parse(e)
	if errC == nil && errE == nil && uC.Hostname() == uE.Hostname() {
		return true
	}
	return false
}

// VerifyRegistration parses and validates the client response during passkey enrollment.
func VerifyRegistration(rawID []byte, clientDataJSONRaw []byte, attestationObjectRaw []byte, expectedChallenge []byte, expectedOrigin, expectedRPID string) (*ParsedAttestation, error) {
	// 1. Validate clientDataJSON
	var clientData ClientData
	if err := json.Unmarshal(clientDataJSONRaw, &clientData); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidClientData, err)
	}

	if clientData.Type != "webauthn.create" {
		return nil, fmt.Errorf("%w: unexpected type %q", ErrInvalidClientData, clientData.Type)
	}

	decodedChallenge, err := Base64URLDecode(clientData.Challenge)
	if err != nil || subtle.ConstantTimeCompare(decodedChallenge, expectedChallenge) != 1 {
		return nil, ErrChallengeMismatch
	}

	if !MatchOrigin(clientData.Origin, expectedOrigin) {
		return nil, fmt.Errorf("%w: client %q != expected %q", ErrOriginMismatch, clientData.Origin, expectedOrigin)
	}

	// 2. Parse attestationObject (CBOR)
	cborVal, _, err := DecodeCBOR(attestationObjectRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: cbor decode: %v", ErrAttestationMalformed, err)
	}
	attMap, ok := cborVal.(map[any]any)
	if !ok {
		return nil, fmt.Errorf("%w: not a map", ErrAttestationMalformed)
	}

	authDataRaw, ok := attMap["authData"].([]byte)
	if !ok || len(authDataRaw) < 37 {
		return nil, fmt.Errorf("%w: authData missing or too short", ErrAttestationMalformed)
	}

	// 3. Verify authData
	rpIdHash := authDataRaw[0:32]
	expectedHash := sha256.Sum256([]byte(expectedRPID))
	if subtle.ConstantTimeCompare(rpIdHash, expectedHash[:]) != 1 {
		return nil, ErrRPIDMismatch
	}

	flags := authDataRaw[32]
	if flags&0x01 == 0 { // UP: User Present
		return nil, ErrUserNotPresent
	}
	if flags&0x40 == 0 { // AT: Attested credential data
		return nil, fmt.Errorf("%w: AT flag not set", ErrAttestationMalformed)
	}

	signCount := binary.BigEndian.Uint32(authDataRaw[33:37])
	if len(authDataRaw) < 37+16+2 {
		return nil, fmt.Errorf("%w: credential data truncated", ErrAttestationMalformed)
	}

	aaguid := authDataRaw[37:53]
	credIdLen := int(binary.BigEndian.Uint16(authDataRaw[53:55]))
	if len(authDataRaw) < 55+credIdLen {
		return nil, fmt.Errorf("%w: credId truncated", ErrAttestationMalformed)
	}

	credentialID := authDataRaw[55 : 55+credIdLen]
	if len(rawID) > 0 && !bytes.Equal(credentialID, rawID) {
		return nil, errors.New("credential ID does not match raw ID")
	}

	coseKeyBytes := authDataRaw[55+credIdLen:]
	coseVal, _, err := DecodeCBOR(coseKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: cose decode: %v", ErrAttestationMalformed, err)
	}

	coseMap, ok := coseVal.(map[any]any)
	if !ok {
		return nil, fmt.Errorf("%w: cose key is not a map", ErrAttestationMalformed)
	}

	// Parse COSE Public Key
	ktyVal, ok := coseMap[int64(1)].(int64)
	if !ok {
		return nil, fmt.Errorf("%w: missing or invalid kty", ErrAttestationMalformed)
	}
	algVal, _ := coseMap[int64(3)].(int64)

	var pubKeyDER []byte
	switch ktyVal {
	case 2: // EC2 (P-256)
		xBytes, okX := coseMap[int64(-2)].([]byte)
		yBytes, okY := coseMap[int64(-3)].([]byte)
		if !okX || !okY {
			return nil, fmt.Errorf("%w: missing EC x or y coordinate", ErrAttestationMalformed)
		}
		ecKey := &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}
		der, err := x509.MarshalPKIXPublicKey(ecKey)
		if err != nil {
			return nil, fmt.Errorf("marshal EC pubkey: %w", err)
		}
		pubKeyDER = der
		if algVal == 0 {
			algVal = -7 // Default ES256
		}

	case 3: // RSA
		nBytes, okN := coseMap[int64(-1)].([]byte)
		eRaw, okE := coseMap[int64(-2)]
		if !okN || !okE {
			return nil, fmt.Errorf("%w: missing RSA n or e", ErrAttestationMalformed)
		}
		var eInt int
		switch e := eRaw.(type) {
		case int64:
			eInt = int(e)
		case []byte:
			eInt = int(new(big.Int).SetBytes(e).Int64())
		}
		rsaKey := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: eInt,
		}
		der, err := x509.MarshalPKIXPublicKey(rsaKey)
		if err != nil {
			return nil, fmt.Errorf("marshal RSA pubkey: %w", err)
		}
		pubKeyDER = der
		if algVal == 0 {
			algVal = -257 // Default RS256
		}

	case 1: // OKP (Ed25519)
		xBytes, okX := coseMap[int64(-2)].([]byte)
		if !okX || len(xBytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: invalid Ed25519 public key", ErrAttestationMalformed)
		}
		edKey := ed25519.PublicKey(xBytes)
		der, err := x509.MarshalPKIXPublicKey(edKey)
		if err != nil {
			return nil, fmt.Errorf("marshal Ed25519 pubkey: %w", err)
		}
		pubKeyDER = der
		algVal = -8

	default:
		return nil, fmt.Errorf("%w: unsupported kty %d", ErrUnsupportedAlgorithm, ktyVal)
	}

	return &ParsedAttestation{
		AAGUID:       aaguid,
		CredentialID: credentialID,
		PublicKey:    pubKeyDER,
		Algorithm:    algVal,
		SignCount:    signCount,
	}, nil
}

// VerifyAssertion validates an authentication assertion (login signature) from the passkey.
func VerifyAssertion(rawID []byte, clientDataJSONRaw []byte, authenticatorData []byte, signature []byte, expectedChallenge []byte, expectedOrigin, expectedRPID string, storedPublicKeyDER []byte, storedSignCount uint32) (uint32, error) {
	// 1. Validate clientDataJSON
	var clientData ClientData
	if err := json.Unmarshal(clientDataJSONRaw, &clientData); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidClientData, err)
	}

	if clientData.Type != "webauthn.get" {
		return 0, fmt.Errorf("%w: unexpected type %q", ErrInvalidClientData, clientData.Type)
	}

	decodedChallenge, err := Base64URLDecode(clientData.Challenge)
	if err != nil || subtle.ConstantTimeCompare(decodedChallenge, expectedChallenge) != 1 {
		return 0, ErrChallengeMismatch
	}

	if !MatchOrigin(clientData.Origin, expectedOrigin) {
		return 0, fmt.Errorf("%w: client %q != expected %q", ErrOriginMismatch, clientData.Origin, expectedOrigin)
	}

	// 2. Validate authenticatorData
	if len(authenticatorData) < 37 {
		return 0, errors.New("authenticatorData too short")
	}

	rpIdHash := authenticatorData[0:32]
	expectedHash := sha256.Sum256([]byte(expectedRPID))
	if subtle.ConstantTimeCompare(rpIdHash, expectedHash[:]) != 1 {
		return 0, ErrRPIDMismatch
	}

	flags := authenticatorData[32]
	if flags&0x01 == 0 { // UP: User Present
		return 0, ErrUserNotPresent
	}

	newSignCount := binary.BigEndian.Uint32(authenticatorData[33:37])
	// Replay warning: If sign count is monotonically increasing, new signCount should exceed stored
	if storedSignCount > 0 && newSignCount > 0 && newSignCount <= storedSignCount {
		// Log or fail for potential cloned authenticator
		return 0, errors.New("authenticator counter regression or replay detected")
	}

	// 3. Verify signature over (authenticatorData || SHA256(clientDataJSON))
	clientDataHash := sha256.Sum256(clientDataJSONRaw)
	signedData := make([]byte, 0, len(authenticatorData)+32)
	signedData = append(signedData, authenticatorData...)
	signedData = append(signedData, clientDataHash[:]...)

	pubKey, err := x509.ParsePKIXPublicKey(storedPublicKeyDER)
	if err != nil {
		return 0, fmt.Errorf("parse stored pubkey: %w", err)
	}

	switch key := pubKey.(type) {
	case *ecdsa.PublicKey:
		hashed := sha256.Sum256(signedData)
		if !ecdsa.VerifyASN1(key, hashed[:], signature) {
			return 0, ErrSignatureInvalid
		}

	case *rsa.PublicKey:
		hashed := sha256.Sum256(signedData)
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashed[:], signature); err != nil {
			return 0, ErrSignatureInvalid
		}

	case ed25519.PublicKey:
		if !ed25519.Verify(key, signedData, signature) {
			return 0, ErrSignatureInvalid
		}

	default:
		return 0, fmt.Errorf("%w: unknown key type %T", ErrUnsupportedAlgorithm, pubKey)
	}

	return newSignCount, nil
}
