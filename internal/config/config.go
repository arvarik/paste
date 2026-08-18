package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains validated runtime limits and security settings.
type Config struct {
	Port                  string
	DataDir               string
	AdminToken            string
	MaxItems              int
	MaxStorageBytes       int64
	MaxItemBytes          int64
	ContentCacheBytes     int64
	SearchIndexBytes      int64
	PreviewCacheBytes     int64
	BackupLimitBytes      int64
	AnonymousRate         int
	AuthenticatedRate     int
	RateBurst             int
	CreateLimitPerHour    int
	DiffWorkers           int
	FormatWorkers         int
	PreviewWorkers        int
	WorkWaitTimeout       time.Duration
	RequireTokenForCreate bool
	TrustedProxies        []netip.Prefix
	DefaultExpiry         time.Duration
	MaxExpiry             time.Duration
}

// Load reads and validates application configuration from the environment.
func Load() (Config, error) {
	adminToken, err := loadAdminToken()
	if err != nil {
		return Config{}, err
	}
	config := Config{
		Port:                  value("PORT", "8083"),
		DataDir:               value("DATA_DIR", "./data"),
		AdminToken:            adminToken,
		MaxItems:              10_000,
		MaxStorageBytes:       1 << 30,
		MaxItemBytes:          2 << 20,
		ContentCacheBytes:     64 << 20,
		SearchIndexBytes:      64 << 20,
		PreviewCacheBytes:     64 << 20,
		BackupLimitBytes:      2 << 30,
		AnonymousRate:         120,
		AuthenticatedRate:     600,
		RateBurst:             30,
		CreateLimitPerHour:    60,
		DiffWorkers:           2,
		FormatWorkers:         2,
		PreviewWorkers:        2,
		WorkWaitTimeout:       2 * time.Second,
		RequireTokenForCreate: false,
		MaxExpiry:             365 * 24 * time.Hour,
	}

	err = nil
	if config.MaxItems, err = integer("PASTE_MAX_ITEMS", config.MaxItems, 1); err != nil {
		return Config{}, err
	}
	if config.MaxStorageBytes, err = bytesValue("PASTE_MAX_STORAGE", config.MaxStorageBytes); err != nil {
		return Config{}, err
	}
	if config.MaxItemBytes, err = bytesValue("PASTE_MAX_ITEM_SIZE", config.MaxItemBytes); err != nil {
		return Config{}, err
	}
	if config.ContentCacheBytes, err = bytesValue("PASTE_CONTENT_CACHE", config.ContentCacheBytes); err != nil {
		return Config{}, err
	}
	if config.SearchIndexBytes, err = bytesValue("PASTE_SEARCH_INDEX", config.SearchIndexBytes); err != nil {
		return Config{}, err
	}
	if config.PreviewCacheBytes, err = bytesValue("PASTE_PREVIEW_CACHE", config.PreviewCacheBytes); err != nil {
		return Config{}, err
	}
	if config.BackupLimitBytes, err = bytesValue("PASTE_BACKUP_LIMIT", config.BackupLimitBytes); err != nil {
		return Config{}, err
	}
	if config.AnonymousRate, err = integer("PASTE_RATE_ANONYMOUS_PER_MINUTE", config.AnonymousRate, 1); err != nil {
		return Config{}, err
	}
	if config.AuthenticatedRate, err = integer("PASTE_RATE_AUTHENTICATED_PER_MINUTE", config.AuthenticatedRate, 1); err != nil {
		return Config{}, err
	}
	if config.RateBurst, err = integer("PASTE_RATE_BURST", config.RateBurst, 1); err != nil {
		return Config{}, err
	}
	if config.CreateLimitPerHour, err = integer("PASTE_CREATE_LIMIT_PER_HOUR", config.CreateLimitPerHour, 1); err != nil {
		return Config{}, err
	}
	if config.DiffWorkers, err = integer("PASTE_DIFF_WORKERS", config.DiffWorkers, 1); err != nil {
		return Config{}, err
	}
	if config.FormatWorkers, err = integer("PASTE_FORMAT_WORKERS", config.FormatWorkers, 1); err != nil {
		return Config{}, err
	}
	if config.PreviewWorkers, err = integer("PASTE_PREVIEW_WORKERS", config.PreviewWorkers, 1); err != nil {
		return Config{}, err
	}
	if config.WorkWaitTimeout, err = duration("PASTE_WORK_WAIT_TIMEOUT", config.WorkWaitTimeout); err != nil {
		return Config{}, err
	}
	if config.RequireTokenForCreate, err = boolean("PASTE_REQUIRE_TOKEN_FOR_CREATE", false); err != nil {
		return Config{}, err
	}
	if config.DefaultExpiry, err = duration("PASTE_DEFAULT_EXPIRY", 0); err != nil {
		return Config{}, err
	}
	if config.MaxExpiry, err = duration("PASTE_MAX_EXPIRY", config.MaxExpiry); err != nil {
		return Config{}, err
	}
	if config.DefaultExpiry < 0 || config.DefaultExpiry > config.MaxExpiry {
		return Config{}, fmt.Errorf("PASTE_DEFAULT_EXPIRY must be between zero and PASTE_MAX_EXPIRY")
	}
	if config.MaxItemBytes > config.MaxStorageBytes {
		return Config{}, fmt.Errorf("PASTE_MAX_ITEM_SIZE cannot exceed PASTE_MAX_STORAGE")
	}
	if config.TrustedProxies, err = prefixes("PASTE_TRUSTED_PROXIES"); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(config.Port) == "" {
		return Config{}, fmt.Errorf("PORT cannot be empty")
	}
	portNumber, portErr := strconv.Atoi(config.Port)
	if portErr != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, fmt.Errorf("PORT must be an integer from 1 to 65535")
	}
	if strings.TrimSpace(config.DataDir) == "" {
		return Config{}, fmt.Errorf("DATA_DIR cannot be empty")
	}
	if config.AdminToken != "" && (len(config.AdminToken) < 32 || len(config.AdminToken) > 256) {
		return Config{}, fmt.Errorf("the administrator token must contain 32 to 256 characters")
	}
	return config, nil
}

func loadAdminToken() (string, error) {
	direct := strings.TrimSpace(os.Getenv("PASTE_ADMIN_TOKEN"))
	path := strings.TrimSpace(os.Getenv("PASTE_ADMIN_TOKEN_FILE"))
	if direct != "" && path != "" {
		return "", fmt.Errorf("set only one of PASTE_ADMIN_TOKEN and PASTE_ADMIN_TOKEN_FILE")
	}
	if path == "" {
		return direct, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read PASTE_ADMIN_TOKEN_FILE: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", fmt.Errorf("PASTE_ADMIN_TOKEN_FILE must be a regular file smaller than 4096 bytes")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read PASTE_ADMIN_TOKEN_FILE: %w", err)
	}
	return strings.TrimSpace(string(content)), nil
}

func value(name, fallback string) string {
	if configured, ok := os.LookupEnv(name); ok {
		return configured
	}
	return fallback
}

func integer(name string, fallback, minimum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < minimum {
		return 0, fmt.Errorf("%s must be an integer greater than or equal to %d", name, minimum)
	}
	return parsed, nil
}

func boolean(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative duration", name)
	}
	return parsed, nil
}

func bytesValue(name string, fallback int64) (int64, error) {
	raw := strings.ToUpper(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback, nil
	}
	multiplier := int64(1)
	for suffix, factor := range map[string]int64{
		"KIB": 1 << 10,
		"MIB": 1 << 20,
		"GIB": 1 << 30,
		"KB":  1_000,
		"MB":  1_000_000,
		"GB":  1_000_000_000,
	} {
		if strings.HasSuffix(raw, suffix) {
			raw = strings.TrimSpace(strings.TrimSuffix(raw, suffix))
			multiplier = factor
			break
		}
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || parsed > (1<<63-1)/multiplier {
		return 0, fmt.Errorf("%s must be a positive byte size", name)
	}
	return parsed * multiplier, nil
}

func prefixes(name string) ([]netip.Prefix, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	values := strings.Split(raw, ",")
	result := make([]netip.Prefix, 0, len(values))
	for _, item := range values {
		item = strings.TrimSpace(item)
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			address, addressErr := netip.ParseAddr(item)
			if addressErr != nil {
				return nil, fmt.Errorf("%s contains invalid address or CIDR %q", name, item)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}
