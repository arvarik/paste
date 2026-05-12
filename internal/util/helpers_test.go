package util

import (
	"testing"
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
		{"abcDEF123", true},
		{"a", true},
		{"Z", true},
		{"9", true},
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
