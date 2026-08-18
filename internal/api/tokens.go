package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arvarik/paste/internal/auth"
)

type principalContextKey struct{}

const maxBearerTokenLength = 256

var apiAuthConfig struct {
	sync.RWMutex
	store      *auth.Store
	adminToken string
}

// ConfigureAPIAuth installs the token store and optional bootstrap admin token.
func ConfigureAPIAuth(store *auth.Store, adminToken string) {
	apiAuthConfig.Lock()
	apiAuthConfig.store = store
	apiAuthConfig.adminToken = strings.TrimSpace(adminToken)
	apiAuthConfig.Unlock()
}

// APIAuthenticationMiddleware validates an optional bearer token.
func APIAuthenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if len(authorization) > maxBearerTokenLength+len("Bearer ") {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid API token"})
			return
		}
		raw := bearerToken(authorization)
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}

		apiAuthConfig.RLock()
		store := apiAuthConfig.store
		adminToken := apiAuthConfig.adminToken
		apiAuthConfig.RUnlock()

		var principal auth.Principal
		switch {
		case adminToken != "" && constantTimeEqual(raw, adminToken):
			principal = auth.Principal{
				TokenID: "bootstrap-admin",
				Name:    "bootstrap-admin",
				Scopes:  map[string]struct{}{"admin": {}},
			}
		case store != nil:
			authenticated, err := store.Authenticate(raw)
			if err != nil {
				respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid API token"})
				return
			}
			principal = authenticated
		default:
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "API tokens are not configured"})
			return
		}

		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PrincipalFromRequest returns an authenticated API principal.
func PrincipalFromRequest(r *http.Request) (auth.Principal, bool) {
	principal, ok := r.Context().Value(principalContextKey{}).(auth.Principal)
	return principal, ok
}

func handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requireScope(w, r, "admin") {
		return
	}
	var request struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := decodeJSONRequest(w, r, &request); err != nil {
		respondJSONDecodeError(w, err)
		return
	}
	store := configuredTokenStore()
	if store == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "API token storage is unavailable"})
		return
	}
	token, raw, err := store.Create(request.Name, request.Scopes, request.ExpiresAt)
	if err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"token": token, "secret": raw})
}

func handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requireScope(w, r, "admin") {
		return
	}
	store := configuredTokenStore()
	if store == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "API token storage is unavailable"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"tokens": store.List()})
}

func handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requireScope(w, r, "admin") {
		return
	}
	store := configuredTokenStore()
	if store == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "API token storage is unavailable"})
		return
	}
	if err := store.Revoke(r.PathValue("id")); err != nil {
		if errors.Is(err, auth.ErrTokenNotFound) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "API token not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to revoke API token"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	principal, ok := PrincipalFromRequest(r)
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication is required"})
		return false
	}
	if !principal.HasScope(scope) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "The API token lacks the required scope"})
		return false
	}
	return true
}

func configuredTokenStore() *auth.Store {
	apiAuthConfig.RLock()
	defer apiAuthConfig.RUnlock()
	return apiAuthConfig.store
}

func bearerToken(header string) string {
	if len(header) > maxBearerTokenLength+len("Bearer ") {
		return ""
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	if len(parts[1]) > maxBearerTokenLength {
		return ""
	}
	return parts[1]
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
