package auth

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenLifecycleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "tokens.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	expiresAt := time.Now().Add(time.Hour).UTC()
	token, raw, err := store.Create("deployment", []string{"write", "read", "write"}, &expiresAt)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if raw == "" || token.Hash != "" {
		t.Fatalf("Create() exposed invalid values: token=%#v raw=%q", token, raw)
	}
	principal, err := store.Authenticate(raw)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !principal.HasScope("write") || principal.HasScope("admin") {
		t.Fatalf("principal scopes = %#v", principal.Scopes)
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload NewStore() error = %v", err)
	}
	if _, err := reloaded.Authenticate(raw); err != nil {
		t.Fatalf("reloaded Authenticate() error = %v", err)
	}
	if err := reloaded.Revoke(token.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := reloaded.Authenticate(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate() after revoke error = %v", err)
	}
}

func TestTokenPersistenceReconcilesCommittedDirectorySyncError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "tokens.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	originalSync := syncTokenDirectory
	failNext := true
	syncTokenDirectory = func(path string) error {
		if failNext {
			failNext = false
			return errors.New("injected directory sync failure")
		}
		return originalSync(path)
	}
	t.Cleanup(func() { syncTokenDirectory = originalSync })
	token, raw, err := store.Create("committed", []string{"read"}, nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := store.Authenticate(raw); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.Authenticate(raw); err != nil {
		t.Fatalf("reloaded Authenticate() error = %v", err)
	}
	failNext = true
	if err := reloaded.Revoke(token.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := reloaded.Authenticate(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate() after revoke error = %v", err)
	}
	secondReload, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondReload.Authenticate(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reloaded revoked token error = %v", err)
	}
}

func TestTokenValidationAndExpiry(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, _, err := store.Create("", []string{"read"}, nil); err == nil {
		t.Fatal("Create() accepted an empty name")
	}
	if _, _, err := store.Create("bad", []string{"root"}, nil); err == nil {
		t.Fatal("Create() accepted an unsupported scope")
	}
	past := time.Now().Add(-time.Minute)
	if _, _, err := store.Create("expired", []string{"read"}, &past); err == nil {
		t.Fatal("Create() accepted an expired token")
	}
	if _, err := store.Authenticate("not-a-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if _, err := store.Authenticate("pst_" + strings.Repeat("x", maxRawTokenLength)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("oversized Authenticate() error = %v", err)
	}
}

func TestAdminScopeGrantsAllScopes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	_, raw, err := store.Create("admin", []string{"admin"}, nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	principal, err := store.Authenticate(raw)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !principal.HasScope("read") || !principal.HasScope("write") || !principal.HasScope("admin") {
		t.Fatal("admin token did not grant all scopes")
	}
}

func TestPublicTokenCopiesDoNotMutateStoredScopes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	token, raw, err := store.Create("reader", []string{"read"}, nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	token.Scopes[0] = "admin"
	listed := store.List()
	listed[0].Scopes[0] = "admin"

	principal, err := store.Authenticate(raw)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.HasScope("admin") || !principal.HasScope("read") {
		t.Fatalf("mutated public scopes changed the stored principal: %#v", principal.Scopes)
	}
}
