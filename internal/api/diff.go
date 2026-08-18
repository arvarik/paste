package api

import (
	"net/http"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

const (
	maxDiffInputBytes = 1 << 20
	maxDiffLines      = 20_000
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
	var req DiffRequest
	if err := decodeJSONRequest(w, r, &req); err != nil {
		respondJSONDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.Base) == "" && strings.TrimSpace(req.Compare) == "" {
		http.Error(w, "Base or compare content is required", http.StatusBadRequest)
		return
	}
	if len(req.Base)+len(req.Compare) > maxDiffInputBytes ||
		strings.Count(req.Base, "\n")+strings.Count(req.Compare, "\n") > maxDiffLines {
		http.Error(w, "Diff input is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !acquireWorkSlot(r.Context(), diffWorkSlots) {
		http.Error(w, "Diff service is busy", http.StatusServiceUnavailable)
		return
	}
	defer releaseWorkSlot(diffWorkSlots)

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

	respondJSON(w, http.StatusOK, resp)
}
