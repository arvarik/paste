package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var errFormatterUnavailable = errors.New("formatter unavailable")

// handleFormatCode formats code based on the specified language.
// Request:  POST /api/format { "content": "...", "language": "go|json|python" }
// Response: 200 OK { "formatted": "..." }
func handleFormatCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content  string `json:"content"`
		Language string `json:"language"`
	}

	if err := decodeJSONRequest(w, r, &req); err != nil {
		respondJSONDecodeError(w, err)
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Content is required",
		})
		return
	}
	if !acquireWorkSlot(r.Context(), formatWorkSlots) {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The formatter is busy"})
		return
	}
	defer releaseWorkSlot(formatWorkSlots)

	var formatted string
	var err error

	switch strings.ToLower(req.Language) {
	case "go":
		formatted, err = formatGo(req.Content)
	case "json":
		formatted, err = formatJSON(req.Content)
	case "python":
		formatted, err = formatPython(r.Context(), req.Content)
	default:
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Auto-format not supported for " + req.Language,
		})
		return
	}

	if err != nil {
		log.Printf("[format] %s formatting failed: %v", req.Language, err)
		status := http.StatusUnprocessableEntity
		message := "Code could not be formatted"
		if errors.Is(err, errFormatterUnavailable) {
			status = http.StatusServiceUnavailable
			message = "The formatter is not available on this server"
		}
		respondJSON(w, status, map[string]string{
			"error": message,
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"formatted": formatted,
	})
}

// formatWithCmd pipes content through an external command with optional arguments.
func formatWithCmd(parent context.Context, content string, args ...string) (string, error) {
	if len(args) == 0 {
		return content, nil
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(content)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("formatter timed out: %w", ctx.Err())
		}
		return "", fmt.Errorf("formatter failed: %w", err)
	}
	return stdout.String(), nil
}

// formatGo formats Go source with the standard library.
func formatGo(content string) (string, error) {
	formatted, err := format.Source([]byte(content))
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}

// formatJSON formats JSON content using Go's encoding/json.
func formatJSON(content string) (string, error) {
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "    "); err != nil {
		return "", err
	}

	return buf.String() + "\n", nil
}

// formatPython attempts to format Python code with an installed formatter.
// It reports errFormatterUnavailable when no formatter exists.
func formatPython(ctx context.Context, content string) (string, error) {
	var formatterErr error
	// Try black (the most popular Python formatter)
	if blackPath, err := exec.LookPath("black"); err == nil {
		formatted, fmtErr := formatWithCmd(ctx, content, blackPath, "-", "--quiet")
		if fmtErr == nil {
			return formatted, nil
		}
		formatterErr = fmtErr
	}

	// Try autopep8
	if autopep8Path, err := exec.LookPath("autopep8"); err == nil {
		formatted, fmtErr := formatWithCmd(ctx, content, autopep8Path, "-")
		if fmtErr == nil {
			return formatted, nil
		}
		formatterErr = fmtErr
	}

	if formatterErr != nil {
		return "", formatterErr
	}
	return "", errFormatterUnavailable
}
