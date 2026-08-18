package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	legacyIDLength = 6
	secureIDLength = 32
	maxTitleLength = 120
)

// langMap maps supported language identifiers to their canonical file extensions.
// This is the single source of truth for language <-> extension conversion.
var langMap = map[string]string{
	"text":       ".txt",
	"python":     ".py",
	"go":         ".go",
	"typescript": ".ts",
	"kotlin":     ".kt",
	"java":       ".java",
	"scala":      ".scala",
	"json":       ".json",
	"bash":       ".sh",
	"markdown":   ".md",
	"html":       ".html",
	"css":        ".css",
}

// LangToExt converts a language name (e.g. "python") to its file extension (e.g. ".py").
// Returns ".txt" if the language is not recognized.
func LangToExt(lang string) string {
	if ext, ok := langMap[strings.ToLower(lang)]; ok {
		return ext
	}
	return ".txt"
}

// NormalizeLanguage returns a supported language name or text.
func NormalizeLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if _, ok := langMap[language]; ok {
		return language
	}
	return "text"
}

// extMap is an automatically generated reverse lookup for langMap.
var extMap map[string]string

func init() {
	extMap = make(map[string]string, len(langMap))
	for lang, ext := range langMap {
		extMap[ext] = lang
	}
}

// ExtToLang converts a file extension (e.g. ".py") to a language name (e.g. "python").
// Returns "text" if the extension is not recognized.
func ExtToLang(ext string) string {
	ext = strings.ToLower(ext)
	if lang, ok := extMap[ext]; ok {
		return lang
	}
	return "text"
}

// IsValidID accepts a legacy six-character ID or a secure 32-character ID.
// Every character must be alphanumeric to prevent path traversal.
func IsValidID(id string) bool {
	if len(id) != legacyIDLength && len(id) != secureIDLength {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// GenerateID creates a cryptographically random six-character identifier.
func GenerateID() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, legacyIDLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("generate random identifier: %w", err)
		}
		b[i] = charset[n.Int64()]
	}
	return string(b), nil
}

// SanitizeTitle converts a title into a portable filename segment.
func SanitizeTitle(title, fallback string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, "&", "and")

	var b strings.Builder
	b.Grow(len(title))
	lastWasDash := false
	for _, r := range title {
		unsafe := unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune(`/\\<>:"|?*`, r)
		if unsafe {
			if b.Len() > 0 && !lastWasDash {
				b.WriteByte('-')
				lastWasDash = true
			}
			continue
		}

		b.WriteRune(r)
		lastWasDash = r == '-'
	}

	result := strings.Trim(b.String(), ".-_ ")
	if result == "" {
		result = fallback
	}

	return truncateUTF8(result, maxTitleLength)
}

// GetPreview returns a cleaned, truncated preview snippet of paste content
// for display in the sidebar. Returns plain text (no HTML escaping);
// the frontend is responsible for escaping before rendering.
func GetPreview(content string) string {
	content = collapseWhitespace(content)
	if utf8.RuneCountInString(content) > 70 {
		return truncateUTF8(content, 70) + "..."
	}
	return content
}

// GetHighlightedPreview returns a plain-text preview snippet centered around
// the first match of query. The frontend is responsible for highlighting.
func GetHighlightedPreview(content, query string) string {
	content = collapseWhitespace(content)

	lowerContent := strings.ToLower(content)
	idx := strings.Index(lowerContent, query)

	if idx == -1 {
		return GetPreview(content)
	}

	// Extract a window around the match for context
	start := utf8BoundaryBefore(content, idx-30)
	prefix := "..."
	if start <= 0 {
		start = 0
		prefix = ""
	}

	end := utf8BoundaryAfter(content, idx+len(query)+40)
	suffix := "..."
	if end >= len(content) {
		end = len(content)
		suffix = ""
	}

	snippet := content[start:end]
	return prefix + snippet + suffix
}

// truncateUTF8 returns at most maxRunes complete UTF-8 characters.
func truncateUTF8(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}

	for index := range value {
		if maxRunes == 0 {
			return value[:index]
		}
		maxRunes--
	}
	return value
}

// utf8BoundaryBefore moves an index to the closest earlier UTF-8 boundary.
func utf8BoundaryBefore(value string, index int) int {
	if index <= 0 {
		return 0
	}
	if index >= len(value) {
		return len(value)
	}
	for index > 0 && !utf8.RuneStart(value[index]) {
		index--
	}
	return index
}

// utf8BoundaryAfter moves an index to the closest later UTF-8 boundary.
func utf8BoundaryAfter(value string, index int) int {
	if index <= 0 {
		return 0
	}
	if index >= len(value) {
		return len(value)
	}
	for index < len(value) && !utf8.RuneStart(value[index]) {
		index++
	}
	return index
}

// collapseWhitespace replaces all newlines with spaces and collapses
// consecutive spaces into a single space for clean preview output.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// GetEnv reads an environment variable or returns a fallback default.
func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
