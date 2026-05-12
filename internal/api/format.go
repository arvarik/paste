package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// handleFormatCode formats code based on the specified language.
// Request:  POST /api/format { "content": "...", "language": "go|json|python" }
// Response: 200 OK { "formatted": "..." }
func handleFormatCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20) // 2MB limit

	var req struct {
		Content  string `json:"content"`
		Language string `json:"language"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Content is required",
		})
		return
	}

	var formatted string
	var err error

	switch strings.ToLower(req.Language) {
	case "go":
		formatted, err = formatWithCmd(req.Content, "gofmt")
	case "json":
		formatted, err = formatJSON(req.Content)
	case "python":
		formatted, err = formatPython(req.Content)
	default:
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Auto-format not supported for " + req.Language,
		})
		return
	}

	if err != nil {
		log.Printf("[format] %s formatting failed: %v", req.Language, err)
		// Return original content on error — formatting should be idempotent
		respondJSON(w, http.StatusOK, map[string]string{
			"formatted": req.Content,
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"formatted": formatted,
	})
}

// formatWithCmd pipes content through an external command with optional arguments.
func formatWithCmd(content string, args ...string) (string, error) {
	if len(args) == 0 {
		return content, nil
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(content)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set a timeout to prevent hanging
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			return "", err
		}
		return stdout.String(), nil
	case <-time.After(10 * time.Second):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return "", exec.ErrNotFound
	}
}

// formatJSON formats JSON content using Go's encoding/json.
func formatJSON(content string) (string, error) {
	// First validate it's valid JSON
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		// Not valid JSON — return as-is (idempotent)
		return content, nil
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "    "); err != nil {
		return content, nil
	}

	return buf.String() + "\n", nil
}

// formatPython attempts to format Python code using available formatters.
// Tries black first, then autopep8. Returns original content if no formatter is available.
func formatPython(content string) (string, error) {
	// Try black (the most popular Python formatter)
	if blackPath, err := exec.LookPath("black"); err == nil {
		formatted, fmtErr := formatWithCmd(content, blackPath, "-", "--quiet")
		if fmtErr == nil {
			return formatted, nil
		}
	}

	// Try autopep8
	if autopep8Path, err := exec.LookPath("autopep8"); err == nil {
		formatted, fmtErr := formatWithCmd(content, autopep8Path, "-")
		if fmtErr == nil {
			return formatted, nil
		}
	}

	// No formatter available — return content as-is (idempotent behavior)
	return content, nil
}
