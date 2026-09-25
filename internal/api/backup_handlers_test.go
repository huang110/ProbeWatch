package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
