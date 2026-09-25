package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/auth/webauthn"
)

func TestWebAuthnLoginBegin(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/login/begin", nil)
	req.Host = "tz.115yu.us.ci"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ChallengeID string `json:"challenge_id"`
		PublicKey   struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ChallengeID == "" || resp.PublicKey.Challenge == "" {
		t.Fatal("empty challenge or challenge_id in response")
	}
	if resp.PublicKey.RPID != "tz.115yu.us.ci" {
		t.Fatalf("expected rpId tz.115yu.us.ci, got %s", resp.PublicKey.RPID)
	}
}

func TestWebAuthnRegisterUnauthorized(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/register/begin", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestWebAuthnFullFlow(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()

	// 1. Create admin user & session
	ctx := t.Context()
	now := time.Now().UTC()
	admin, err := store.UpsertAdminUser(ctx, "local", "admin", "admin", now)
	if err != nil {
		t.Fatalf("upsert admin: %v", err)
	}

	sessionValue, err := service.CreateSessionForUser(ctx, admin.ID, now)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionCookie := &http.Cookie{Name: "probewatch_session", Value: sessionValue}

	// Fetch CSRF token
	csrfReq := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	csrfReq.AddCookie(sessionCookie)
	csrfRec := httptest.NewRecorder()
	handler.ServeHTTP(csrfRec, csrfReq)
	if csrfRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for csrf, got %d: %s", csrfRec.Code, csrfRec.Body.String())
	}
	var csrfResp struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(csrfRec.Body).Decode(&csrfResp)
	csrfToken := csrfResp.Token

	// 2. Register Begin
	regBeginReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/webauthn/register/begin", bytes.NewReader([]byte("{}")))
	regBeginReq.Host = "127.0.0.1:8080"
	regBeginReq.AddCookie(sessionCookie)
	regBeginReq.Header.Set("Content-Type", "application/json")
	regBeginReq.Header.Set("Origin", "http://127.0.0.1:8080")
	regBeginReq.Header.Set("X-CSRF-Token", csrfToken)
	regBeginRec := httptest.NewRecorder()

	handler.ServeHTTP(regBeginRec, regBeginReq)
	if regBeginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for register begin, got %d: %s", regBeginRec.Code, regBeginRec.Body.String())
	}

	var regBeginResp struct {
		ChallengeID string `json:"challenge_id"`
		PublicKey   struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	if err := json.NewDecoder(regBeginRec.Body).Decode(&regBeginResp); err != nil {
		t.Fatalf("decode reg begin: %v", err)
	}

	// 3. Synthesize registration credential using EC P-256
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pub := privKey.PublicKey
	pad32 := func(b []byte) []byte {
		if len(b) >= 32 {
			return b
		}
		res := make([]byte, 32)
		copy(res[32-len(b):], b)
		return res
	}
	xBytes := pad32(pub.X.Bytes())
	yBytes := pad32(pub.Y.Bytes())

	var coseKey []byte
	coseKey = append(coseKey, 0xa5)
	coseKey = append(coseKey, 0x01, 0x02)
	coseKey = append(coseKey, 0x03, 0x26)
	coseKey = append(coseKey, 0x20, 0x01)
	coseKey = append(coseKey, 0x21, 0x58, 0x20)
	coseKey = append(coseKey, xBytes...)
	coseKey = append(coseKey, 0x22, 0x58, 0x20)
	coseKey = append(coseKey, yBytes...)

	rpId := regBeginResp.PublicKey.RP.ID
	rpIdHash := sha256.Sum256([]byte(rpId))
	rawCredID := []byte("test-passkey-cred-id-999")
	aaguid := make([]byte, 16)

	var authData []byte
	authData = append(authData, rpIdHash[:]...)
	authData = append(authData, 0x45) // UP(0x01) | UV(0x04) | AT(0x40)
	authData = append(authData, 0x00, 0x00, 0x00, 0x01) // signCount = 1
	authData = append(authData, aaguid...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(rawCredID)))
	authData = append(authData, rawCredID...)
	authData = append(authData, coseKey...)

	var attObj []byte
	attObj = append(attObj, 0xa2)
	attObj = append(attObj, 0x63, 'f', 'm', 't', 0x64, 'n', 'o', 'n', 'e')
	attObj = append(attObj, 0x68, 'a', 'u', 't', 'h', 'D', 'a', 't', 'a')
	if len(authData) <= 255 {
		attObj = append(attObj, 0x58, byte(len(authData)))
	} else {
		attObj = append(attObj, 0x59, byte(len(authData)>>8), byte(len(authData)&0xff))
	}
	attObj = append(attObj, authData...)

	clientDataReg, _ := json.Marshal(webauthn.ClientData{
		Type:      "webauthn.create",
		Challenge: regBeginResp.PublicKey.Challenge,
		Origin:    "http://127.0.0.1:8080",
	})

	regFinishPayload, _ := json.Marshal(map[string]any{
		"challenge_id": regBeginResp.ChallengeID,
		"name":         "Windows Hello (测试)",
		"id":           webauthn.Base64URLEncode(rawCredID),
		"rawId":        webauthn.Base64URLEncode(rawCredID),
		"type":         "public-key",
		"response": map[string]any{
			"clientDataJSON":    webauthn.Base64URLEncode(clientDataReg),
			"attestationObject": webauthn.Base64URLEncode(attObj),
		},
	})

	// Fetch fresh CSRF token for mutating request
	csrfReq2 := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	csrfReq2.AddCookie(sessionCookie)
	csrfRec2 := httptest.NewRecorder()
	handler.ServeHTTP(csrfRec2, csrfReq2)
	_ = json.NewDecoder(csrfRec2.Body).Decode(&csrfResp)

	regFinishReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/webauthn/register/finish", bytes.NewReader(regFinishPayload))
	regFinishReq.Host = "127.0.0.1:8080"
	regFinishReq.AddCookie(sessionCookie)
	regFinishReq.Header.Set("Content-Type", "application/json")
	regFinishReq.Header.Set("Origin", "http://127.0.0.1:8080")
	regFinishReq.Header.Set("X-CSRF-Token", csrfResp.Token)
	regFinishRec := httptest.NewRecorder()

	handler.ServeHTTP(regFinishRec, regFinishReq)
	if regFinishRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for register finish, got %d: %s", regFinishRec.Code, regFinishRec.Body.String())
	}

	// 4. List credentials
	listReq := httptest.NewRequest(http.MethodGet, "/api/webauthn/credentials", nil)
	listReq.AddCookie(sessionCookie)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for credentials list, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var credsList []map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&credsList); err != nil {
		t.Fatalf("decode credentials list: %v", err)
	}
	if len(credsList) != 1 || credsList[0]["name"] != "Windows Hello (测试)" {
		t.Fatalf("unexpected creds list: %+v", credsList)
	}
	createdCredID := credsList[0]["id"].(string)

	// 5. Test Passkey Login
	loginBeginReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/webauthn/login/begin", nil)
	loginBeginReq.Host = "127.0.0.1:8080"
	loginBeginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginBeginRec, loginBeginReq)

	if loginBeginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for login begin, got %d: %s", loginBeginRec.Code, loginBeginRec.Body.String())
	}
	var loginBeginResp struct {
		ChallengeID string `json:"challenge_id"`
		PublicKey   struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	_ = json.NewDecoder(loginBeginRec.Body).Decode(&loginBeginResp)

	clientDataLogin, _ := json.Marshal(webauthn.ClientData{
		Type:      "webauthn.get",
		Challenge: loginBeginResp.PublicKey.Challenge,
		Origin:    "http://127.0.0.1:8080",
	})

	var authDataLogin []byte
	authDataLogin = append(authDataLogin, rpIdHash[:]...)
	authDataLogin = append(authDataLogin, 0x05) // UP(1) | UV(4)
	authDataLogin = append(authDataLogin, 0x00, 0x00, 0x00, 0x02) // signCount = 2

	clientDataHash := sha256.Sum256(clientDataLogin)
	signedData := append(authDataLogin, clientDataHash[:]...)
	dataHash := sha256.Sum256(signedData)

	signature, err := ecdsa.SignASN1(rand.Reader, privKey, dataHash[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	loginFinishPayload, _ := json.Marshal(map[string]any{
		"challenge_id": loginBeginResp.ChallengeID,
		"id":           webauthn.Base64URLEncode(rawCredID),
		"rawId":        webauthn.Base64URLEncode(rawCredID),
		"type":         "public-key",
		"response": map[string]any{
			"clientDataJSON":    webauthn.Base64URLEncode(clientDataLogin),
			"authenticatorData": webauthn.Base64URLEncode(authDataLogin),
			"signature":         webauthn.Base64URLEncode(signature),
		},
	})

	loginFinishReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/webauthn/login/finish", bytes.NewReader(loginFinishPayload))
	loginFinishReq.Host = "127.0.0.1:8080"
	loginFinishReq.Header.Set("Content-Type", "application/json")
	loginFinishReq.Header.Set("Origin", "http://127.0.0.1:8080")
	loginFinishRec := httptest.NewRecorder()

	handler.ServeHTTP(loginFinishRec, loginFinishReq)
	if loginFinishRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for login finish, got %d: %s", loginFinishRec.Code, loginFinishRec.Body.String())
	}

	// Verify session cookie was issued
	cookies := loginFinishRec.Result().Cookies()
	var foundSession bool
	for _, c := range cookies {
		if c.Name == "probewatch_session" && c.Value != "" {
			foundSession = true
			break
		}
	}
	if !foundSession {
		t.Fatal("expected probewatch_session cookie in login finish response")
	}

	// 6. Delete Credential
	csrfReq3 := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	csrfReq3.AddCookie(sessionCookie)
	csrfRec3 := httptest.NewRecorder()
	handler.ServeHTTP(csrfRec3, csrfReq3)
	_ = json.NewDecoder(csrfRec3.Body).Decode(&csrfResp)

	delReq := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:8080/api/webauthn/credentials/"+createdCredID, nil)
	delReq.Host = "127.0.0.1:8080"
	delReq.AddCookie(sessionCookie)
	delReq.Header.Set("Content-Type", "application/json")
	delReq.Header.Set("Origin", "http://127.0.0.1:8080")
	delReq.Header.Set("X-CSRF-Token", csrfResp.Token)
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for delete cred, got %d: %s", delRec.Code, delRec.Body.String())
	}
}
