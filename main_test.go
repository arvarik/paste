package main

import (
	"os"
	"testing"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		fallback string
		envValue *string // nil means the env var should not be set
		expected string
	}{
		{
			name:     "existing key",
			key:      "TEST_ENV_VAR",
			fallback: "fallback_value",
			envValue: ptr("test_value"),
			expected: "test_value",
		},
		{
			name:     "missing key",
			key:      "TEST_ENV_MISSING",
			fallback: "fallback_value",
			envValue: nil,
			expected: "fallback_value",
		},
		{
			name:     "empty value",
			key:      "TEST_ENV_EMPTY",
			fallback: "fallback_value",
			envValue: ptr(""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable
			if tt.envValue != nil {
				os.Setenv(tt.key, *tt.envValue)
			} else {
				os.Unsetenv(tt.key) // ensure it's not set
			}

			// Clean up environment variable after test
			t.Cleanup(func() {
				os.Unsetenv(tt.key)
			})

			// Test function
			result := getEnv(tt.key, tt.fallback)

			if result != tt.expected {
				t.Errorf("getEnv(%q, %q) = %q, want %q", tt.key, tt.fallback, result, tt.expected)
			}
		})
	}
}

// ptr returns a pointer to the given string.
func ptr(s string) *string {
	return &s
}
