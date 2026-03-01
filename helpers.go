package main

import (
	"crypto/rand"
	"html"
	"math/big"
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

// langToExt converts a language name (e.g. "python") to its file extension (e.g. ".py").
// Returns ".txt" if the language is not recognized.
func langToExt(lang string) string {
	if ext, ok := langMap[strings.ToLower(lang)]; ok {
		return ext
	}
	return ".txt"
}

// extToLang converts a file extension (e.g. ".py") to a language name (e.g. "python").
// Returns "text" if the extension is not recognized.
func extToLang(ext string) string {
	ext = strings.ToLower(ext)
	for lang, e := range langMap {
		if e == ext {
			return lang
		}
	}
	return "text"
}

// generateID creates a cryptographically random 6-character alphanumeric string
// suitable for use as a paste identifier. Uses crypto/rand to prevent ID prediction.
func generateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// getPreview returns a cleaned, truncated preview snippet of paste content
// for display in the sidebar. Limited to 70 characters with HTML escaping.
func getPreview(content string) string {
	content = collapseWhitespace(content)
	if len(content) > 70 {
		return html.EscapeString(content[:70]) + "..."
	}
	return html.EscapeString(content)
}

// getHighlightedPreview returns a preview snippet with the search query
// wrapped in <mark> tags for visual highlighting. Falls back to getPreview
// if the query is not found in the content.
func getHighlightedPreview(content, query string) string {
	content = collapseWhitespace(content)

	lowerContent := strings.ToLower(content)
	idx := strings.Index(lowerContent, query)

	if idx == -1 {
		return getPreview(content)
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
	escapedSnippet := html.EscapeString(snippet)
	escapedQuery := html.EscapeString(query)

	re := regexp.MustCompile("(?i)(" + regexp.QuoteMeta(escapedQuery) + ")")
	highlighted := re.ReplaceAllString(escapedSnippet, `<mark class="bg-yellow-200 dark:bg-yellow-500/40 text-gray-900 dark:text-white rounded px-0.5">$1</mark>`)

	return prefix + highlighted + suffix
}

// collapseWhitespace replaces all newlines with spaces and collapses
// consecutive spaces into a single space for clean preview output.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
