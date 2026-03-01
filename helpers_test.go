package main

import "testing"

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
			if got := isValidID(tt.id); got != tt.expected {
				t.Errorf("isValidID(%q) = %v, want %v", tt.id, got, tt.expected)
			}
		})
	}
}
