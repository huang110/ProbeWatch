package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

// TestNodeScopeAuthorization covers multi-tenant node authorization across subroutes.
func TestNodeScopeAuthorization(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()

	now := time.Now().UTC()

	// 1. Create two registered nodes
	regToken1, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg1 := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          "550e8400-e29b-41d4-a716-446655440001",
		Name:              "Allowed-Node-1",
		RegistrationToken: regToken1.Token,
	})

	regToken2, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg2 := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          "550e8400-e29b-41d4-a716-446655440002",
		Name:              "Forbidden-Node-2",
		RegistrationToken: regToken2.Token,
	})

	allowedUUID := reg1.NodeUUID
	forbiddenUUID := reg2.NodeUUID

	// 2. Create admin user first, then create restricted operator user who ONLY has access to node1
	_, err = store.UpsertAdminUser(context.Background(), "local", "root-admin", "admin", now)
	if err != nil {
		t.Fatal(err)
	}

	operatorUser, err := store.UpsertAdminUser(context.Background(), "local", "operator-1", "operator", now)
	if err != nil {
		t.Fatal(err)
	}
	role := db.RoleOperator
	allowedStr := allowedUUID
	_, err = store.UpdateAdminUser(context.Background(), operatorUser.ID, db.UpdateUserInput{
		Role:         &role,
		AllowedNodes: &allowedStr,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	operatorSession, err := service.CreateSessionForUser(context.Background(), operatorUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	operatorCookie := task4SessionCookie(operatorSession)
	operatorCSRF := task4CSRF(t, handler, operatorSession)

	// Admin user with full access
	adminSession, _ := task4AdminSession(t, service, store)
	adminCookie := task4SessionCookie(adminSession)

	// Subroutes to test for GET (read access)
	readSubroutes := []string{
		"/api/nodes/%s",
		"/api/nodes/%s/resource",
		"/api/nodes/%s/network",
		"/api/nodes/%s/containers",
		"/api/nodes/%s/processes",
		"/api/nodes/%s/events",
		"/api/nodes/%s/logs/query",
		"/api/nodes/%s/checks/summary",
		"/api/nodes/%s/traffic",
		"/api/nodes/%s/billing",
		"/api/nodes/%s/ipqa",
		"/api/nodes/%s/ipqa/history",
		"/api/nodes/%s/ipqa/changes",
	}

	for _, pattern := range readSubroutes {
		allowedPath := strings.Replace(pattern, "%s", allowedUUID, 1)
		forbiddenPath := strings.Replace(pattern, "%s", forbiddenUUID, 1)

		// Test A: Operator reading allowed node -> 200
		t.Run("AllowedRead_"+pattern, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, allowedPath, nil)
			req.AddCookie(operatorCookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				if pattern == "/api/nodes/%s/logs/query" && rec.Code == http.StatusServiceUnavailable {
					return
				}
				t.Fatalf("expected 200 for allowed node on %s, got %d: %s", allowedPath, rec.Code, rec.Body.String())
			}
		})

		// Test B: Operator reading forbidden node -> 403 Forbidden
		t.Run("ForbiddenRead_"+pattern, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, forbiddenPath, nil)
			req.AddCookie(operatorCookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 Forbidden for unallowed node on %s, got %d: %s", forbiddenPath, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "access to node denied by scope") {
				t.Fatalf("expected scope denial message, got %s", rec.Body.String())
			}
		})

		// Test C: Unauthenticated request -> 401 Unauthorized
		t.Run("UnauthRead_"+pattern, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, allowedPath, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 Unauthorized on %s, got %d", allowedPath, rec.Code)
			}
		})

		// Test D: Non-existent node UUID -> 404
		t.Run("NotFoundRead_"+pattern, func(t *testing.T) {
			nonExistentPath := strings.Replace(pattern, "%s", "550e8400-e29b-41d4-a716-446655449999", 1)
			req := httptest.NewRequest(http.MethodGet, nonExistentPath, nil)
			req.AddCookie(adminCookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 NotFound on %s, got %d", nonExistentPath, rec.Code)
			}
		})
	}

	// 3. Write operations: Operator attempting to modify forbidden node -> 403
	t.Run("WriteOperationsBlockedForUnallowedNode", func(t *testing.T) {
		writeTests := []struct {
			name   string
			method string
			path   string
			body   string
		}{
			{
				name:   "PutNodeBilling",
				method: http.MethodPut,
				path:   "/api/nodes/" + forbiddenUUID + "/billing",
				body:   `{"reset_day":1,"accounting_method":"total"}`,
			},
			{
				name:   "ResetNodeBilling",
				method: http.MethodPost,
				path:   "/api/nodes/" + forbiddenUUID + "/billing/reset",
				body:   `{}`,
			},
			{
				name:   "PostIPQASync",
				method: http.MethodPost,
				path:   "/api/nodes/" + forbiddenUUID + "/ipqa/sync",
				body:   `{}`,
			},
			{
				name:   "PostIPQATest",
				method: http.MethodPost,
				path:   "/api/nodes/" + forbiddenUUID + "/ipqa/test",
				body:   `{"force_send":true}`,
			},
			{
				name:   "PatchNode",
				method: http.MethodPatch,
				path:   "/api/nodes/" + forbiddenUUID,
				body:   `{"name":"Hacked Name"}`,
			},
		}

		for _, wt := range writeTests {
			// Even with valid CSRF header, forbidden node must be rejected with 403
			req := httptest.NewRequest(wt.method, wt.path, strings.NewReader(wt.body))
			req.AddCookie(operatorCookie)
			req.Header.Set("X-CSRF-Token", operatorCSRF)
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("[%s] expected 403 Forbidden for unauthorized write, got %d: %s", wt.name, rec.Code, rec.Body.String())
			}
		}
	})

	// 4. Verify Database failure returns 503 Service Unavailable
	t.Run("DatabaseErrorReturns503", func(t *testing.T) {
		if _, err := store.DB().Exec("DROP TABLE nodes"); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+allowedUUID+"/billing", nil)
		req.AddCookie(adminCookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable on db failure, got %d", rec.Code)
		}
	})
}
