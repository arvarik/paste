package models

import "time"

// PasteMeta represents the metadata returned by the list and search API endpoints.
type PasteMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Language  string    `json:"language"`
	CreatedAt time.Time `json:"createdAt"`
	Preview   string    `json:"preview"`
	LineCount int       `json:"lineCount"`
}

// CachedPaste holds the metadata and full text content of a single paste.
type CachedPaste struct {
	ID            string
	Title         string
	TitleLower    string
	Content       string
	ContentLower  string
	Language      string
	LanguageLower string
	CreatedAt     time.Time
	Preview       string
	LineCount     int
}

// DiffMeta represents the metadata returned by list/search API endpoints.
type DiffMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// CachedDiff holds the metadata and full text content of a single diff.
type CachedDiff struct {
	ID             string
	Title          string
	TitleLower     string
	Base           string
	Compare        string
	BaseContent    string
	CompareContent string
	ContentLower   string
	CreatedAt      time.Time
}

// DiffData represents the on-disk format for a saved diff.
type DiffData struct {
	Base           string `json:"base"`
	Compare        string `json:"compare"`
	BaseContent    string `json:"baseContent"`
	CompareContent string `json:"compareContent"`
}
