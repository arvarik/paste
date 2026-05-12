package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// DiffRequest represents the JSON payload for the POST /api/diff endpoint.
type DiffRequest struct {
	Base    string `json:"base"`
	Compare string `json:"compare"`
}

// DiffResponse is the structured JSON response for custom UI rendering.
type DiffResponse struct {
	OpCodes      []OpCodeInfo `json:"opCodes"`
	BaseLines    []string     `json:"baseLines"`
	CompareLines []string     `json:"compareLines"`
}

// OpCodeInfo describes a block of changed/unchanged lines.
type OpCodeInfo struct {
	Tag string `json:"tag"` // "equal", "replace", "delete", "insert"
	I1  int    `json:"i1"`
	I2  int    `json:"i2"`
	J1  int    `json:"j1"`
	J2  int    `json:"j2"`
}

// handleDiff computes a structured diff between two text blocks.
// Request:  POST /api/diff {"base": "...", "compare": "..."}
// Response: 200 OK application/json
func handleDiff(w http.ResponseWriter, r *http.Request) {
	// Limit request body to prevent abuse
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

	var req DiffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	baseLines := strings.Split(req.Base, "\n")
	compareLines := strings.Split(req.Compare, "\n")

	matcher := difflib.NewMatcher(baseLines, compareLines)
	opCodes := matcher.GetOpCodes()

	var opCodeInfos []OpCodeInfo
	for _, op := range opCodes {
		tag := ""
		switch op.Tag {
		case 'e':
			tag = "equal"
		case 'r':
			tag = "replace"
		case 'd':
			tag = "delete"
		case 'i':
			tag = "insert"
		}
		opCodeInfos = append(opCodeInfos, OpCodeInfo{
			Tag: tag,
			I1:  op.I1,
			I2:  op.I2,
			J1:  op.J1,
			J2:  op.J2,
		})
	}

	resp := DiffResponse{
		OpCodes:      opCodeInfos,
		BaseLines:    baseLines,
		CompareLines: compareLines,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
	}
}
