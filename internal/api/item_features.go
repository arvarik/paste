package api

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/arvarik/paste/internal/storage"
)

const (
	defaultPageSize  = 50
	maximumPageSize  = 250
	maximumTags      = 32
	maximumTagRunes  = 64
	editSecretHeader = "X-Edit-Secret"
)

// ItemFeatureConfig controls item creation and expiry policies.
type ItemFeatureConfig struct {
	RequireTokenForCreate bool
	DefaultExpiry         time.Duration
	MaximumExpiry         time.Duration
}

var itemFeatureConfig struct {
	sync.RWMutex
	value ItemFeatureConfig
}

// ConfigureItemFeatures replaces the item creation and expiry policies.
func ConfigureItemFeatures(config ItemFeatureConfig) {
	itemFeatureConfig.Lock()
	itemFeatureConfig.value = config
	itemFeatureConfig.Unlock()
}

func currentItemFeatureConfig() ItemFeatureConfig {
	itemFeatureConfig.RLock()
	defer itemFeatureConfig.RUnlock()
	return itemFeatureConfig.value
}

func principalCanWrite(request *http.Request) bool {
	principal, ok := PrincipalFromRequest(request)
	return ok && principal.HasScope("write")
}

func requireCreatePermission(writer http.ResponseWriter, request *http.Request) bool {
	if !currentItemFeatureConfig().RequireTokenForCreate {
		return true
	}
	if principalCanWrite(request) {
		return true
	}
	if _, authenticated := PrincipalFromRequest(request); authenticated {
		respondJSON(writer, http.StatusForbidden, map[string]string{"error": "The API token lacks the write scope"})
	} else {
		respondJSON(writer, http.StatusUnauthorized, map[string]string{"error": "A write API token is required"})
	}
	return false
}

func requestEditSecret(request *http.Request) string {
	secret := request.Header.Get(editSecretHeader)
	if len(secret) > 128 {
		return ""
	}
	return strings.TrimSpace(secret)
}

func validateItemOptions(tags []string, expiresAt *time.Time, creating bool) ([]string, *time.Time, error) {
	if len(tags) > maximumTags {
		return nil, nil, errors.New("too many tags")
	}
	normalizedTags := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || utf8.RuneCountInString(tag) > maximumTagRunes {
			return nil, nil, errors.New("each tag must contain 1 to 64 characters")
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalizedTags = append(normalizedTags, tag)
	}

	config := currentItemFeatureConfig()
	now := time.Now().UTC()
	validatedExpiry := expiresAt
	if creating && validatedExpiry == nil && config.DefaultExpiry > 0 {
		value := now.Add(config.DefaultExpiry)
		validatedExpiry = &value
	}
	if validatedExpiry != nil {
		value := validatedExpiry.UTC()
		if !value.After(now) {
			return nil, nil, errors.New("expiry must be in the future")
		}
		if config.MaximumExpiry > 0 && value.After(now.Add(config.MaximumExpiry)) {
			return nil, nil, errors.New("expiry exceeds the configured maximum")
		}
		validatedExpiry = &value
	}
	return normalizedTags, validatedExpiry, nil
}

func parsePageRequest(request *http.Request) (string, int, error) {
	cursor := strings.TrimSpace(request.URL.Query().Get("cursor"))
	limit := defaultPageSize
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximumPageSize {
			return "", 0, errors.New("limit must be between 1 and 250")
		}
		limit = parsed
	}
	return cursor, limit, nil
}

func parseItemFilter(request *http.Request) (storage.ItemFilter, error) {
	filter := storage.ItemFilter{Tag: strings.TrimSpace(request.URL.Query().Get("tag"))}
	if utf8.RuneCountInString(filter.Tag) > maximumTagRunes {
		return storage.ItemFilter{}, errors.New("tag must contain at most 64 characters")
	}
	if raw, present := request.URL.Query()["favorite"]; present {
		if len(raw) != 1 {
			return storage.ItemFilter{}, errors.New("favorite must occur once")
		}
		value, err := strconv.ParseBool(raw[0])
		if err != nil {
			return storage.ItemFilter{}, errors.New("favorite must be true or false")
		}
		filter.Favorite = &value
	}
	return filter, nil
}

func respondStorageError(writer http.ResponseWriter, err error, noun string) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		respondJSON(writer, http.StatusNotFound, map[string]string{"error": noun + " not found"})
	case errors.Is(err, storage.ErrExpired):
		respondJSON(writer, http.StatusGone, map[string]string{"error": noun + " expired"})
	case errors.Is(err, storage.ErrUnauthorized):
		respondJSON(writer, http.StatusForbidden, map[string]string{"error": "The edit secret is missing or invalid"})
	case errors.Is(err, storage.ErrQuotaExceeded):
		respondJSON(writer, http.StatusInsufficientStorage, map[string]string{"error": "The storage quota is full"})
	case errors.Is(err, storage.ErrConflict):
		respondJSON(writer, http.StatusConflict, map[string]string{"error": "The item changed. Reload it before you save again"})
	default:
		respondJSON(writer, http.StatusInternalServerError, map[string]string{"error": "The server could not complete the storage operation"})
	}
}
