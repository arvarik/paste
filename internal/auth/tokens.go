package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var syncTokenDirectory = func(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

const (
	tokenPrefix       = "pst_"
	maxRawTokenLength = 256
	maxStoredTokens   = 10_000
	maxTokenFileBytes = 16 << 20
)

var (
	ErrInvalidToken  = errors.New("invalid API token")
	ErrTokenNotFound = errors.New("API token not found")
)

// Token describes one API token without exposing its secret value.
type Token struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Hash      string     `json:"hash,omitempty"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

// Principal is the authenticated identity stored in a request context.
type Principal struct {
	TokenID string
	Name    string
	Scopes  map[string]struct{}
}

// HasScope reports whether the principal grants the requested scope.
func (p Principal) HasScope(scope string) bool {
	_, admin := p.Scopes["admin"]
	_, allowed := p.Scopes[scope]
	return admin || allowed
}

// Store persists hashed API tokens in one private JSON file.
type Store struct {
	mu     sync.RWMutex
	path   string
	tokens map[string]Token
	hashes map[string]string
}

// NewStore loads a token store from path.
func NewStore(path string) (*Store, error) {
	store := &Store{path: path, tokens: make(map[string]Token), hashes: make(map[string]string)}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	if info, err := os.Lstat(s.path); err == nil {
		if !info.Mode().IsRegular() || info.Size() > maxTokenFileBytes {
			return errors.New("API token store must be a bounded regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect API token store: %w", err)
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read API token store: %w", err)
	}

	var tokens []Token
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("decode API token store: %w", err)
	}
	if len(tokens) > maxStoredTokens {
		return errors.New("API token store exceeds the token limit")
	}
	for _, token := range tokens {
		if token.ID == "" || token.Hash == "" {
			return errors.New("API token store contains an incomplete record")
		}
		if _, exists := s.tokens[token.ID]; exists {
			return fmt.Errorf("API token store contains duplicate ID %q", token.ID)
		}
		if _, exists := s.hashes[token.Hash]; exists {
			return errors.New("API token store contains a duplicate token hash")
		}
		s.tokens[token.ID] = token
		s.hashes[token.Hash] = token.ID
	}
	return nil
}

// Create generates one API token. The raw value is returned only once.
func (s *Store) Create(name string, scopes []string, expiresAt *time.Time) (Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return Token{}, "", errors.New("token name must contain 1 to 120 characters")
	}
	normalizedScopes, err := normalizeScopes(scopes)
	if err != nil {
		return Token{}, "", err
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return Token{}, "", errors.New("token expiry must be in the future")
	}
	var storedExpiry *time.Time
	if expiresAt != nil {
		expiry := expiresAt.UTC()
		storedExpiry = &expiry
	}

	idBytes := make([]byte, 12)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return Token{}, "", fmt.Errorf("generate token ID: %w", err)
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return Token{}, "", fmt.Errorf("generate token secret: %w", err)
	}

	raw := tokenPrefix + base64.RawURLEncoding.EncodeToString(secretBytes)
	token := Token{
		ID:        base64.RawURLEncoding.EncodeToString(idBytes),
		Name:      name,
		Hash:      hashToken(raw),
		Scopes:    normalizedScopes,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: storedExpiry,
	}

	s.mu.Lock()
	if len(s.tokens) >= maxStoredTokens {
		s.mu.Unlock()
		return Token{}, "", errors.New("API token limit reached")
	}
	s.tokens[token.ID] = token
	s.hashes[token.Hash] = token.ID
	if err := s.persistLocked(); err != nil {
		delete(s.tokens, token.ID)
		delete(s.hashes, token.Hash)
		s.mu.Unlock()
		return Token{}, "", err
	}
	s.mu.Unlock()
	return publicToken(token), raw, nil
}

// Authenticate validates a raw API token.
func (s *Store) Authenticate(raw string) (Principal, error) {
	if !strings.HasPrefix(raw, tokenPrefix) || len(raw) < len(tokenPrefix)+32 || len(raw) > maxRawTokenLength {
		return Principal{}, ErrInvalidToken
	}
	hash := hashToken(raw)
	now := time.Now()

	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.hashes[hash]
	if !ok {
		return Principal{}, ErrInvalidToken
	}
	token, ok := s.tokens[id]
	if !ok || subtle.ConstantTimeCompare([]byte(token.Hash), []byte(hash)) != 1 {
		return Principal{}, ErrInvalidToken
	}
	if token.RevokedAt != nil || token.ExpiresAt != nil && !token.ExpiresAt.After(now) {
		return Principal{}, ErrInvalidToken
	}
	principal := Principal{TokenID: token.ID, Name: token.Name, Scopes: make(map[string]struct{}, len(token.Scopes))}
	for _, scope := range token.Scopes {
		principal.Scopes[scope] = struct{}{}
	}
	return principal, nil
}

// Revoke prevents all future use of a token.
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.tokens[id]
	if !ok {
		return ErrTokenNotFound
	}
	if token.RevokedAt == nil {
		original := token
		now := time.Now().UTC()
		token.RevokedAt = &now
		s.tokens[id] = token
		if err := s.persistLocked(); err != nil {
			s.tokens[id] = original
			return err
		}
	}
	return nil
}

// List returns token metadata without secret hashes.
func (s *Store) List() []Token {
	s.mu.RLock()
	tokens := make([]Token, 0, len(s.tokens))
	for _, token := range s.tokens {
		tokens = append(tokens, publicToken(token))
	}
	s.mu.RUnlock()
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].CreatedAt.After(tokens[j].CreatedAt) })
	return tokens
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create API token directory: %w", err)
	}
	tokens := make([]Token, 0, len(s.tokens))
	for _, token := range s.tokens {
		tokens = append(tokens, token)
	}
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].ID < tokens[j].ID })
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("encode API token store: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".tokens-*")
	if err != nil {
		return fmt.Errorf("create API token temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace API token store: %w", err)
	}
	if err := syncTokenDirectory(filepath.Dir(s.path)); err != nil {
		committed, readErr := os.ReadFile(s.path)
		if readErr != nil || !bytes.Equal(committed, data) {
			return errors.Join(err, readErr)
		}
	}
	return nil
}

func normalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}
	allowed := map[string]struct{}{"read": {}, "write": {}, "admin": {}}
	unique := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if _, ok := allowed[scope]; !ok {
			return nil, fmt.Errorf("unsupported token scope %q", scope)
		}
		unique[scope] = struct{}{}
	}
	normalized := make([]string, 0, len(unique))
	for scope := range unique {
		normalized = append(normalized, scope)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func hashToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func publicToken(token Token) Token {
	token.Hash = ""
	token.Scopes = append([]string(nil), token.Scopes...)
	if token.ExpiresAt != nil {
		expiry := *token.ExpiresAt
		token.ExpiresAt = &expiry
	}
	if token.RevokedAt != nil {
		revoked := *token.RevokedAt
		token.RevokedAt = &revoked
	}
	return token
}
