package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDiff(t *testing.T) {
	tests := []struct {
		name         string
		reqBody      interface{}
		expectedCode int
		expectedTags []string
	}{
		{
			name: "Valid diff generation",
			reqBody: DiffRequest{
				Base:    "hello\nworld",
				Compare: "hello\nbeautiful\nworld",
			},
			expectedCode: http.StatusOK,
			expectedTags: []string{"equal", "insert", "equal"},
		},
		{
			name:         "Invalid JSON body",
			reqBody:      "not a json object",
			expectedCode: http.StatusBadRequest,
			expectedTags: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.reqBody)
			req := httptest.NewRequest(http.MethodPost, "/api/diff", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handleDiff(w, req)

			res := w.Result()
			if res.StatusCode != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, res.StatusCode)
			}

			if tt.expectedCode == http.StatusOK {
				var resp DiffResponse
				if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(resp.OpCodes) != len(tt.expectedTags) {
					t.Fatalf("expected %d opcodes, got %d", len(tt.expectedTags), len(resp.OpCodes))
				}
				for i, tag := range tt.expectedTags {
					if resp.OpCodes[i].Tag != tag {
						t.Errorf("expected tag %q at index %d, got %q", tag, i, resp.OpCodes[i].Tag)
					}
				}
			}
		})
	}
}
