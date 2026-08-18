package util

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCollapseWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no whitespace",
			input:    "helloworld",
			expected: "helloworld",
		},
		{
			name:     "single spaces",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "multiple spaces",
			input:    "hello   world",
			expected: "hello world",
		},
		{
			name:     "newlines",
			input:    "hello\nworld",
			expected: "hello world",
		},
		{
			name:     "multiple newlines",
			input:    "hello\n\nworld",
			expected: "hello world",
		},
		{
			name:     "tabs",
			input:    "hello\tworld",
			expected: "hello world",
		},
		{
			name:     "mixed whitespace",
			input:    "  \t hello \n  \t world  \n  ",
			expected: "hello world",
		},
		{
			name:     "unicode whitespace",
			input:    "hello\u200bworld", // zero-width space is NOT considered whitespace by strings.Fields, so let's stick to standard ones
			expected: "hello\u200bworld",
		},
		{
			name:     "ideographic space",
			input:    "hello\u3000world", // \u3000 is considered whitespace
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collapseWhitespace(tt.input)
			if result != tt.expected {
				t.Errorf("collapseWhitespace(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidID(t *testing.T) {
	tests := []struct {
		id       string
		expected bool
	}{
		{"abc123", true},
		{"ABCDEF", true},
		{"123456", true},
		{"0123456789abcdef0123456789abcdef", true},
		{"0123456789ABCDEF0123456789ABCDEF", true},
		{"0123456789abcdef0123456789abcde!", false},
		{"abcDEF123", false},
		{"a", false},
		{"Z", false},
		{"9", false},
		{"", false},
		{"*", false},
		{"?", false},
		{"[", false},
		{"]", false},
		{".", false},
		{"/", false},
		{"\\", false},
		{"abc*def", false},
		{"abc/def", false},
		{"abc.def", false},
		{"abc-def", false},
		{"abc_def", false},
		{"  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := IsValidID(tt.id); got != tt.expected {
				t.Errorf("IsValidID(%q) = %v, want %v", tt.id, got, tt.expected)
			}
		})
	}
}

func TestGenerateID(t *testing.T) {
	id, err := GenerateID()
	if err != nil {
		t.Fatalf("GenerateID() returned an error: %v", err)
	}
	if !IsValidID(id) {
		t.Fatalf("GenerateID() = %q, want a valid six-character ID", id)
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		expected string
	}{
		{name: "trim and replace", input: "  My & First/Paste  ", fallback: "Untitled", expected: "My-and-First-Paste"},
		{name: "control characters", input: "line\nzero\x00end", fallback: "Untitled", expected: "line-zero-end"},
		{name: "empty after cleanup", input: "<>:\"|?*", fallback: "Untitled", expected: "Untitled"},
		{name: "unicode", input: "Résumé 日本語", fallback: "Untitled", expected: "Résumé-日本語"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeTitle(tt.input, tt.fallback); got != tt.expected {
				t.Fatalf("SanitizeTitle(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetPreviewKeepsValidUTF8(t *testing.T) {
	content := strings.Repeat("界", 80)
	preview := GetPreview(content)
	if !utf8.ValidString(preview) {
		t.Fatalf("GetPreview() returned invalid UTF-8: %q", preview)
	}
	if utf8.RuneCountInString(preview) != 73 {
		t.Fatalf("GetPreview() rune count = %d, want 73", utf8.RuneCountInString(preview))
	}
}
