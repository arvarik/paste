package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATA_DIR", "/tmp/paste-test")
	t.Setenv("PASTE_MAX_STORAGE", "2GiB")
	t.Setenv("PASTE_CONTENT_CACHE", "32MiB")
	t.Setenv("PASTE_SEARCH_INDEX", "16MiB")
	t.Setenv("PASTE_DIFF_WORKERS", "4")
	t.Setenv("PASTE_WORK_WAIT_TIMEOUT", "1500ms")
	t.Setenv("PASTE_REQUIRE_TOKEN_FOR_CREATE", "true")
	t.Setenv("PASTE_DEFAULT_EXPIRY", "24h")
	t.Setenv("PASTE_MAX_EXPIRY", "720h")
	t.Setenv("PASTE_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Port != "9090" || config.DataDir != "/tmp/paste-test" {
		t.Fatalf("Load() basic values = %#v", config)
	}
	if config.MaxStorageBytes != 2<<30 || config.ContentCacheBytes != 32<<20 {
		t.Fatalf("Load() byte limits = %#v", config)
	}
	if config.SearchIndexBytes != 16<<20 || config.DiffWorkers != 4 || config.WorkWaitTimeout != 1500*time.Millisecond {
		t.Fatalf("Load() work limits = %#v", config)
	}
	if !config.RequireTokenForCreate || config.DefaultExpiry != 24*time.Hour {
		t.Fatalf("Load() security values = %#v", config)
	}
	if len(config.TrustedProxies) != 2 {
		t.Fatalf("Load() trusted proxies = %#v", config.TrustedProxies)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("PASTE_MAX_ITEMS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted zero maximum items")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-port")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid port")
	}
}

func TestLoadRejectsShortAdminToken(t *testing.T) {
	t.Setenv("PASTE_ADMIN_TOKEN", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a short bootstrap administrator token")
	}
}

func TestLoadReadsAdminTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-token")
	token := "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PASTE_ADMIN_TOKEN_FILE", path)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.AdminToken != token {
		t.Fatalf("AdminToken = %q", config.AdminToken)
	}
}

func TestLoadRejectsTwoAdminTokenSources(t *testing.T) {
	t.Setenv("PASTE_ADMIN_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("PASTE_ADMIN_TOKEN_FILE", filepath.Join(t.TempDir(), "token"))
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted two administrator token sources")
	}
}

func TestBytesValue(t *testing.T) {
	t.Setenv("SIZE", "12MB")
	value, err := bytesValue("SIZE", 1)
	if err != nil {
		t.Fatalf("bytesValue() error = %v", err)
	}
	if value != 12_000_000 {
		t.Fatalf("bytesValue() = %d", value)
	}
}
