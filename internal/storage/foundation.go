package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/arvarik/paste/internal/models"
)

const (
	metadataSchemaVersion = 1
	metadataFileName      = "metadata.json"
	itemsDirectoryName    = "items"
	revisionsDirectory    = "revisions"
	defaultSearchLimit    = 100
	maxPageLimit          = 250
	secretBytes           = 32
	idBytes               = 16
)

var (
	ErrQuotaExceeded = errors.New("storage quota exceeded")
	ErrUnauthorized  = errors.New("invalid edit secret")
	ErrExpired       = errors.New("stored item expired")
	ErrCorrupt       = errors.New("stored item is corrupt")
	ErrConflict      = errors.New("stored item revision conflict")
)

// StorageLimits bounds storage and search work.
type StorageLimits struct {
	MaxTotalBytes         int64
	MaxItemBytes          int64
	MaxItems              int
	MaxSearchResults      int
	MaxSearchIndexBytes   int
	MaxCachedContentBytes int64
	MaxBackupBytes        int64
}

var (
	limitsMu sync.RWMutex
	limits   = StorageLimits{
		MaxTotalBytes:         1 << 30,
		MaxItemBytes:          4 << 20,
		MaxItems:              10_000,
		MaxSearchResults:      defaultSearchLimit,
		MaxSearchIndexBytes:   8 << 20,
		MaxCachedContentBytes: 16 << 20,
		MaxBackupBytes:        2 << 30,
	}
	usageMu    sync.Mutex
	usageRoot  string
	usageBytes int64
	usageItems int
	usageReady bool
)

// SetStorageLimits replaces the storage limits. Non-positive values disable a limit.
func SetStorageLimits(next StorageLimits) {
	limitsMu.Lock()
	limits = next
	limitsMu.Unlock()
}

// GetStorageLimits returns a copy of the current storage limits.
func GetStorageLimits() StorageLimits {
	limitsMu.RLock()
	defer limitsMu.RUnlock()
	return limits
}

// CreateOptions configures durable metadata for a new item.
type CreateOptions struct {
	EditSecret    string
	Tags          []string
	Favorite      bool
	ExpiresAt     *time.Time
	BurnAfterRead bool
}

// MetadataPatch changes optional item metadata.
type MetadataPatch struct {
	Tags             *[]string
	Favorite         *bool
	ExpiresAt        **time.Time
	BurnAfterRead    *bool
	ExpectedRevision *int64
}

// ItemFilter applies metadata filters before cursor pagination.
type ItemFilter struct {
	Tag      string
	Favorite *bool
}

func matchesItemFilter(tags []string, favorite bool, filter ItemFilter) bool {
	if filter.Favorite != nil && favorite != *filter.Favorite {
		return false
	}
	if filter.Tag == "" {
		return true
	}
	for _, tag := range tags {
		if strings.EqualFold(tag, filter.Tag) {
			return true
		}
	}
	return false
}

// Page contains one stable cursor page.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type cursorValue struct {
	CreatedAt int64  `json:"createdAt"`
	ID        string `json:"id"`
}

func itemRoot(kind models.ItemKind) string {
	return filepath.Join(DataDir, itemsDirectoryName, string(kind)+"s")
}

func itemDirectory(kind models.ItemKind, id string) string {
	return filepath.Join(itemRoot(kind), id)
}

func itemMetadataPath(kind models.ItemKind, id string) string {
	return filepath.Join(itemDirectory(kind, id), metadataFileName)
}

func itemDataPath(metadata models.ItemMetadata) string {
	return filepath.Join(itemDirectory(metadata.Kind, metadata.ID), metadata.DataFile)
}

func revisionRoot(kind models.ItemKind, id string) string {
	return filepath.Join(DataDir, revisionsDirectory, string(kind)+"s", id)
}

func revisionPath(kind models.ItemKind, id string, revision int64) string {
	return filepath.Join(revisionRoot(kind, id), fmt.Sprintf("%020d.json", revision))
}

func validStorageID(id string) bool {
	if len(id) != 6 && len(id) != idBytes*2 {
		return false
	}
	for _, char := range id {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return false
		}
	}
	return true
}

func generateStorageID() (string, error) {
	value := make([]byte, idBytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate storage ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func generateEditSecret() (string, error) {
	value := make([]byte, secretBytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate edit secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashEditSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate edit-secret salt: %w", err)
	}
	digest := sha256.Sum256(append(append([]byte{}, salt...), []byte(secret)...))
	return "sha256$" + base64.RawURLEncoding.EncodeToString(salt) + "$" + base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func verifySecretHash(encoded, secret string) bool {
	if encoded == "" {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "sha256" || secret == "" {
		return false
	}
	salt, saltErr := base64.RawURLEncoding.DecodeString(parts[1])
	want, hashErr := base64.RawURLEncoding.DecodeString(parts[2])
	if saltErr != nil || hashErr != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256(append(append([]byte{}, salt...), []byte(secret)...))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// VerifyEditSecret checks the edit secret for one item.
func VerifyEditSecret(kind models.ItemKind, id, secret string) (bool, error) {
	metadata, err := readMetadata(kind, id)
	if err != nil {
		return false, err
	}
	return verifySecretHash(metadata.EditSecretHash, secret), nil
}

// GetItemMetadata returns one durable metadata document.
func GetItemMetadata(kind models.ItemKind, id string) (models.ItemMetadata, error) {
	return readMetadata(kind, id)
}

func checksumBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func canonicalTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || utf8.RuneCountInString(tag) > 64 {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, tag)
		if len(result) == 32 {
			break
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToLower(result[left]) < strings.ToLower(result[right])
	})
	return result
}

func normalizeMetadata(metadata *models.ItemMetadata) error {
	if metadata.SchemaVersion != metadataSchemaVersion {
		return fmt.Errorf("%w: unsupported metadata schema %d", ErrCorrupt, metadata.SchemaVersion)
	}
	if metadata.Kind != models.ItemKindPaste && metadata.Kind != models.ItemKindDiff {
		return fmt.Errorf("%w: invalid item kind", ErrCorrupt)
	}
	if !validStorageID(metadata.ID) {
		return fmt.Errorf("%w: invalid item ID", ErrCorrupt)
	}
	if metadata.Title == "" || metadata.CreatedAt.IsZero() || metadata.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: incomplete metadata", ErrCorrupt)
	}
	if metadata.Revision < 1 || metadata.Size < 0 || metadata.DataFile == "" || filepath.Base(metadata.DataFile) != metadata.DataFile {
		return fmt.Errorf("%w: invalid item metadata", ErrCorrupt)
	}
	metadata.Tags = canonicalTags(metadata.Tags)
	return nil
}

func readMetadata(kind models.ItemKind, id string) (models.ItemMetadata, error) {
	if !validStorageID(id) {
		return models.ItemMetadata{}, fs.ErrNotExist
	}
	content, err := readStorageFile(itemMetadataPath(kind, id), 1<<20)
	if err != nil {
		return models.ItemMetadata{}, err
	}
	var metadata models.ItemMetadata
	if err := json.Unmarshal(content, &metadata); err != nil {
		return models.ItemMetadata{}, fmt.Errorf("%w: decode metadata: %v", ErrCorrupt, err)
	}
	if metadata.Kind != kind || metadata.ID != id {
		return models.ItemMetadata{}, fmt.Errorf("%w: metadata path mismatch", ErrCorrupt)
	}
	if err := normalizeMetadata(&metadata); err != nil {
		return models.ItemMetadata{}, err
	}
	return metadata, nil
}

func writeMetadata(metadata models.ItemMetadata) error {
	if err := normalizeMetadata(&metadata); err != nil {
		return err
	}
	content, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	path := itemMetadataPath(metadata.Kind, metadata.ID)
	return reconcileCommittedFile(path, content, replaceFileAtomically(path, content, time.Time{}))
}

func storageUsage() (int64, int, error) {
	var total int64
	var count int
	for _, kind := range []models.ItemKind{models.ItemKindPaste, models.ItemKindDiff} {
		entries, err := os.ReadDir(itemRoot(kind))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return 0, 0, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !validStorageID(entry.Name()) {
				continue
			}
			metadata, err := readMetadata(kind, entry.Name())
			if err != nil {
				continue
			}
			total += metadata.Size
			count++
		}
	}
	revisionBase := filepath.Join(DataDir, revisionsDirectory)
	err := filepath.WalkDir(revisionBase, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symbolic link in revision storage", ErrCorrupt)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: non-regular revision file", ErrCorrupt)
		}
		total += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, 0, err
	}
	return total, count, nil
}

func checkAdditionalStorage(delta int64) error {
	configured := GetStorageLimits()
	usageMu.Lock()
	defer usageMu.Unlock()
	if !usageReady || usageRoot != DataDir {
		total, count, err := storageUsage()
		if err != nil {
			return err
		}
		usageRoot, usageBytes, usageItems, usageReady = DataDir, total, count, true
	}
	projected := usageBytes + delta
	if projected < 0 {
		projected = 0
	}
	if configured.MaxTotalBytes > 0 && projected > configured.MaxTotalBytes {
		return fmt.Errorf("%w: total size exceeds %d bytes", ErrQuotaExceeded, configured.MaxTotalBytes)
	}
	return nil
}

func checkQuota(oldSize, newSize int64, newItem bool) error {
	configured := GetStorageLimits()
	if configured.MaxItemBytes > 0 && newSize > configured.MaxItemBytes {
		return fmt.Errorf("%w: item uses %d bytes", ErrQuotaExceeded, newSize)
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	if !usageReady || usageRoot != DataDir {
		total, count, err := storageUsage()
		if err != nil {
			return err
		}
		usageRoot, usageBytes, usageItems, usageReady = DataDir, total, count, true
	}
	total, count := usageBytes, usageItems
	if newItem {
		count++
	}
	if configured.MaxItems > 0 && count > configured.MaxItems {
		return fmt.Errorf("%w: item count exceeds %d", ErrQuotaExceeded, configured.MaxItems)
	}
	projected := total - oldSize + newSize
	if configured.MaxTotalBytes > 0 && projected > configured.MaxTotalBytes {
		return fmt.Errorf("%w: total size exceeds %d bytes", ErrQuotaExceeded, configured.MaxTotalBytes)
	}
	return nil
}

func adjustUsage(oldSize, newSize int64, itemDelta int) {
	usageMu.Lock()
	if usageReady && usageRoot == DataDir {
		usageBytes += newSize - oldSize
		usageItems += itemDelta
	}
	usageMu.Unlock()
}

func resetUsage() {
	usageMu.Lock()
	usageRoot = ""
	usageBytes = 0
	usageItems = 0
	usageReady = false
	usageMu.Unlock()
}

func buildSearchText(parts ...string) string {
	configured := GetStorageLimits()
	limit := configured.MaxSearchIndexBytes
	if limit <= 0 {
		return ""
	}
	if limit > 8<<10 {
		limit = 8 << 10
	}
	var builder strings.Builder
	for _, part := range parts {
		remaining := limit - builder.Len()
		if remaining <= 0 {
			break
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
			remaining--
		}
		if remaining <= 0 {
			break
		}
		part = strings.ToLower(part)
		if len(part) > remaining {
			part = part[:remaining]
		}
		builder.WriteString(part)
	}
	return builder.String()
}

func effectiveLimit(requested int) int {
	configured := GetStorageLimits().MaxSearchResults
	if configured <= 0 || configured > maxPageLimit {
		configured = maxPageLimit
	}
	if requested <= 0 || requested > configured {
		return configured
	}
	return requested
}

func encodeCursor(createdAt time.Time, id string) string {
	content, _ := json.Marshal(cursorValue{CreatedAt: createdAt.UnixNano(), ID: id})
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeCursor(cursor string) (cursorValue, error) {
	if cursor == "" {
		return cursorValue{}, nil
	}
	content, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return cursorValue{}, fmt.Errorf("invalid cursor: %w", err)
	}
	var value cursorValue
	if err := json.Unmarshal(content, &value); err != nil || value.ID == "" {
		return cursorValue{}, errors.New("invalid cursor")
	}
	return value, nil
}

func cursorStart[T any](items []T, cursor string, key func(T) (time.Time, string)) (int, error) {
	value, err := decodeCursor(cursor)
	if err != nil || cursor == "" {
		return 0, err
	}
	for index, item := range items {
		createdAt, id := key(item)
		itemTime := createdAt.UnixNano()
		if itemTime < value.CreatedAt || (itemTime == value.CreatedAt && id > value.ID) {
			return index, nil
		}
	}
	return len(items), nil
}

func revisionNumberFromName(name string) (int64, bool) {
	if filepath.Ext(name) != ".json" {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSuffix(name, ".json"), 10, 64)
	return value, err == nil && value > 0
}
