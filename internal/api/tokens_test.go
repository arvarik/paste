package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arvarik/paste/internal/auth"
)

func TestAPITokenHTTPFlow(t *testing.T) {
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("auth.NewStore() error = %v", err)
	}
	ConfigureAPIAuth(store, "bootstrap-secret")
	t.Cleanup(func() { ConfigureAPIAuth(nil, "") })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tokens", handleCreateAPIToken)
	mux.HandleFunc("GET /api/tokens", handleListAPITokens)
	mux.HandleFunc("DELETE /api/tokens/{id}", handleRevokeAPIToken)
	handler := APIAuthenticationMiddleware(mux)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized list = %d, want 401", unauthorized.Code)
	}

	create := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/tokens", strings.NewReader(`{"name":"automation","scopes":["write"]}`))
	request.Header.Set("Authorization", "Bearer bootstrap-secret")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(create, request)
	if create.Code != http.StatusCreated {
		t.Fatalf("create token = %d, want 201: %s", create.Code, create.Body.String())
	}
	var response struct {
		Token  auth.Token `json:"token"`
		Secret string     `json:"secret"`
	}
	if err := json.NewDecoder(create.Body).Decode(&response); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if response.Secret == "" || response.Token.ID == "" {
		t.Fatalf("token response = %#v", response)
	}

	principalRecorder := httptest.NewRecorder()
	principalHandler := APIAuthenticationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromRequest(r)
		if !ok || !principal.HasScope("write") {
			t.Fatal("request has no write principal")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+response.Secret)
	principalHandler.ServeHTTP(principalRecorder, request)
	if principalRecorder.Code != http.StatusNoContent {
		t.Fatalf("authenticated request = %d, want 204", principalRecorder.Code)
	}

	revoke := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/tokens/"+response.Token.ID, nil)
	request.Header.Set("Authorization", "Bearer bootstrap-secret")
	handler.ServeHTTP(revoke, request)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke token = %d, want 204", revoke.Code)
	}
}

func TestInvalidBearerTokenFailsClosed(t *testing.T) {
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("auth.NewStore() error = %v", err)
	}
	ConfigureAPIAuth(store, "")
	t.Cleanup(func() { ConfigureAPIAuth(nil, "") })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer invalid")
	APIAuthenticationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("invalid token reached the protected handler")
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token = %d, want 401", recorder.Code)
	}
}

func TestOversizedBearerTokenFailsBeforeAuthentication(t *testing.T) {
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	ConfigureAPIAuth(store, "")
	t.Cleanup(func() { ConfigureAPIAuth(nil, "") })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("x", maxBearerTokenLength+1))
	APIAuthenticationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized token reached the protected handler")
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("oversized token = %d, want 401", recorder.Code)
	}
}
