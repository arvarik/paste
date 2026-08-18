package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestFormatHandlerFormatsGoAndJSON(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()

	goResponse := dxRequest(t, handler, http.MethodPost, "/api/format", map[string]any{
		"language": "go", "content": "package main\nfunc main(){println(1)}",
	}, nil)
	if goResponse.Code != http.StatusOK {
		t.Fatalf("Go format = %d: %s", goResponse.Code, goResponse.Body.String())
	}
	var formatted map[string]string
	if err := json.NewDecoder(goResponse.Body).Decode(&formatted); err != nil {
		t.Fatal(err)
	}
	if formatted["formatted"] != "package main\n\nfunc main() { println(1) }\n" {
		t.Fatalf("formatted Go = %q", formatted["formatted"])
	}

	jsonResponse := dxRequest(t, handler, http.MethodPost, "/api/format", map[string]any{
		"language": "json", "content": `{"value":1}`,
	}, nil)
	if jsonResponse.Code != http.StatusOK {
		t.Fatalf("JSON format = %d: %s", jsonResponse.Code, jsonResponse.Body.String())
	}
}

func TestFormatHandlerRejectsInvalidOrUnsupportedCode(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()

	invalid := dxRequest(t, handler, http.MethodPost, "/api/format", map[string]any{
		"language": "go", "content": "package",
	}, nil)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid Go = %d, want 422", invalid.Code)
	}
	unsupported := dxRequest(t, handler, http.MethodPost, "/api/format", map[string]any{
		"language": "ruby", "content": "puts 1",
	}, nil)
	if unsupported.Code != http.StatusBadRequest {
		t.Fatalf("unsupported format = %d, want 400", unsupported.Code)
	}
}
