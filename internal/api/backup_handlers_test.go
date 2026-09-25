package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBackupHandlersCRUDAndSecurity(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Unauthenticated list returns 401
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/system/backups", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d, want 401", unauth.Code)
	}

	// 2. Create backup without CSRF returns 403
	noCSRF := task4AdminWrite(t, handler, http.MethodPost, session, "", "/api/system/backups", "{}")
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("create backup without CSRF = %d, want 403", noCSRF.Code)
	}

	// 3. Create backup with CSRF returns 201
	created, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups", "{}")
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup status = %d, body = %s", created.Code, created.Body.String())
	}
	var backup map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &backup); err != nil {
		t.Fatalf("unmarshal backup response: %v", err)
	}
	filename, ok := backup["filename"].(string)
	if !ok || filename == "" {
		t.Fatalf("missing filename in backup response: %+v", backup)
	}

	// 4. List backups
	listReq := httptest.NewRequest(http.MethodGet, "/api/system/backups", nil)
	listReq.AddCookie(task4SessionCookie(session))
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list backups status = %d", listRec.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected at least 1 backup in list")
	}

	// 5. Download backup
	dlReq := httptest.NewRequest(http.MethodGet, "/api/system/backups/"+filename+"/download", nil)
	dlReq.AddCookie(task4SessionCookie(session))
	dlRec := httptest.NewRecorder()
	handler.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("download backup status = %d", dlRec.Code)
	}
	if dlRec.Body.Len() == 0 {
		t.Fatal("downloaded backup is empty")
	}

	// 6. Restore backup
	restoreRes, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups/"+filename+"/restore", "{}")
	if restoreRes.Code != http.StatusOK {
		t.Fatalf("restore status = %d, body = %s", restoreRes.Code, restoreRes.Body.String())
	}

	// 7. Traversal attempt rejected (refresh CSRF from restored session)
	csrf = task4CSRF(t, handler, session)
	travRes, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups/invalid..file.db/restore", "{}")
	if travRes.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, body = %s, want 400", travRes.Code, travRes.Body.String())
	}

	// 7b. Invalid download rejected
	badDlReq := httptest.NewRequest(http.MethodGet, "/api/system/backups/bad..name.db.gz/download", nil)
	badDlReq.AddCookie(task4SessionCookie(session))
	badDlRec := httptest.NewRecorder()
	handler.ServeHTTP(badDlRec, badDlReq)
	if badDlRec.Code != http.StatusBadRequest {
		t.Fatalf("bad download status = %d, want 400", badDlRec.Code)
	}

	// 8. Delete backup
	csrf = task4CSRF(t, handler, session)
	delRes, _ := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/system/backups/"+filename, "{}")
	if delRes.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", delRes.Code, delRes.Body.String())
	}
}

func TestBackupConfigAndRemoteOperations(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Get default backup config
	cfgReq := httptest.NewRequest(http.MethodGet, "/api/system/backups/config", nil)
	cfgReq.AddCookie(task4SessionCookie(session))
	cfgRec := httptest.NewRecorder()
	handler.ServeHTTP(cfgRec, cfgReq)
	if cfgRec.Code != http.StatusOK {
		t.Fatalf("get backup config status = %d, body = %s", cfgRec.Code, cfgRec.Body.String())
	}
	var bCfg map[string]any
	if err := json.Unmarshal(cfgRec.Body.Bytes(), &bCfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if bCfg["interval_hours"].(float64) != 24 {
		t.Fatalf("interval_hours = %v, want 24", bCfg["interval_hours"])
	}

	// 2. Update config with S3 and WebDAV
	updatePayload := `{
		"enabled": true,
		"interval_hours": 12,
		"retention_count": 5,
		"s3": {
			"enabled": false,
			"endpoint": "https://s3.us-west-000.backblazeb2.com",
			"bucket": "my-bucket",
			"region": "us-west-000",
			"access_key": "AKIA12345",
			"secret_key": "supersecretkey987"
		},
		"webdav": {
			"enabled": false,
			"url": "https://dav.example.com/remote.php/webdav",
			"username": "davuser",
			"password": "davpassword456"
		}
	}`
	csrf = task4CSRF(t, handler, session)
	saveRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups/config", updatePayload)
	if saveRec.Code != http.StatusOK {
		t.Fatalf("save backup config status = %d, body = %s", saveRec.Code, saveRec.Body.String())
	}

	// 3. Verify config is saved and masked
	cfgRec2 := httptest.NewRecorder()
	cfgReq2 := httptest.NewRequest(http.MethodGet, "/api/system/backups/config", nil)
	cfgReq2.AddCookie(task4SessionCookie(session))
	handler.ServeHTTP(cfgRec2, cfgReq2)
	var bCfg2 map[string]any
	_ = json.Unmarshal(cfgRec2.Body.Bytes(), &bCfg2)
	s3Map, _ := bCfg2["s3"].(map[string]any)
	if !strings.Contains(s3Map["secret_key"].(string), "••••") || s3Map["secret_key"] == "supersecretkey987" {
		t.Fatalf("s3 secret_key not masked: %v", s3Map["secret_key"])
	}
	davMap, _ := bCfg2["webdav"].(map[string]any)
	if !strings.Contains(davMap["password"].(string), "••••") || davMap["password"] == "davpassword456" {
		t.Fatalf("webdav password not masked: %v", davMap["password"])
	}

	// 4. Create a backup for dry-run verification
	csrf = task4CSRF(t, handler, session)
	createRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups", "{}")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create backup status = %d", createRec.Code)
	}
	var backupInfo map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &backupInfo)
	filename := backupInfo["filename"].(string)

	// 5. Test dry-run verification
	csrf = task4CSRF(t, handler, session)
	verifyRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups/"+filename+"/verify", "{}")
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify backup status = %d, body = %s", verifyRec.Code, verifyRec.Body.String())
	}
	var verifyResult map[string]any
	_ = json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult)
	if verifyResult["integrity"] != "ok" {
		t.Fatalf("expected integrity ok, got %v", verifyResult["integrity"])
	}

	// 6. Test export-now
	csrf = task4CSRF(t, handler, session)
	exportRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/system/backups/export-now", "{}")
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export-now status = %d, body = %s", exportRec.Code, exportRec.Body.String())
	}
}
