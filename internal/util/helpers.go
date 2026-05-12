package util

import (
	"crypto/rand"
	"math/big"
	"os"
	"regexp"
	"strings"
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

// extMap is an automatically generated reverse lookup for langMap.
var extMap map[string]string

// TitleSanitizer replaces characters in paste titles that could cause path traversal
// or filesystem issues, replacing them with safe alternatives.
var TitleSanitizer *strings.Replacer

func init() {
	extMap = make(map[string]string, len(langMap))
	for lang, ext := range langMap {
		extMap[ext] = lang
	}
	TitleSanitizer = strings.NewReplacer(
		"/", "_",
		"\\", "_",
		" ", "-",
		"<", "",
		">", "",
		"\"", "",
		"'", "",
		"&", "and",
	)
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

// IsValidID checks if the provided string contains only alphanumeric characters.
// This is used to validate IDs and prevent path traversal and globbing attacks.
func IsValidID(id string) bool {
	if len(id) == 0 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// GenerateID creates a cryptographically random 6-character alphanumeric string
// suitable for use as a paste identifier. Uses crypto/rand to prevent ID prediction.
func GenerateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// GetPreview returns a cleaned, truncated preview snippet of paste content
// for display in the sidebar. Returns plain text (no HTML escaping);
// the frontend is responsible for escaping before rendering.
func GetPreview(content string) string {
	content = collapseWhitespace(content)
	if len(content) > 70 {
		return content[:70] + "..."
	}
	return content
}

// GetHighlightedPreview returns a plain-text preview snippet centered around
// the first match of query. The frontend is responsible for highlighting.
func GetHighlightedPreview(content, query string, re *regexp.Regexp) string {
	content = collapseWhitespace(content)

	lowerContent := strings.ToLower(content)
	idx := strings.Index(lowerContent, query)

	if idx == -1 {
		return GetPreview(content)
	}

	// Extract a window around the match for context
	start := idx - 30
	prefix := "..."
	if start <= 0 {
		start = 0
		prefix = ""
	}

	end := idx + len(query) + 40
	suffix := "..."
	if end >= len(content) {
		end = len(content)
		suffix = ""
	}

	snippet := content[start:end]
	return prefix + snippet + suffix
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
