package models

import "time"

// ItemKind identifies one stored item family.
type ItemKind string

const (
	ItemKindPaste ItemKind = "paste"
	ItemKindDiff  ItemKind = "diff"
)

// ItemMetadata is the durable metadata shared by pastes and diffs.
// DataFile is relative to the item directory.
type ItemMetadata struct {
	SchemaVersion  int        `json:"schemaVersion"`
	Kind           ItemKind   `json:"kind"`
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Language       string     `json:"language,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	EditSecretHash string     `json:"editSecretHash"`
	Tags           []string   `json:"tags"`
	Favorite       bool       `json:"favorite"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	BurnAfterRead  bool       `json:"burnAfterRead"`
	Revision       int64      `json:"revision"`
	Size           int64      `json:"size"`
	Checksum       string     `json:"checksum"`
	DataFile       string     `json:"dataFile"`
}

// PasteMeta represents paste metadata returned by list and search functions.
type PasteMeta struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Language      string     `json:"language"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	Preview       string     `json:"preview"`
	LineCount     int        `json:"lineCount"`
	Tags          []string   `json:"tags,omitempty"`
	Favorite      bool       `json:"favorite,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	BurnAfterRead bool       `json:"burnAfterRead,omitempty"`
	Revision      int64      `json:"revision,omitempty"`
	Size          int64      `json:"size,omitempty"`
}

// CachedPaste holds one paste and its search fields.
// ContentLower remains for source compatibility. Storage does not populate it.
type CachedPaste struct {
	ID             string
	Title          string
	TitleLower     string
	Content        string
	ContentLower   string
	Language       string
	LanguageLower  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Preview        string
	LineCount      int
	Tags           []string
	Favorite       bool
	ExpiresAt      *time.Time
	BurnAfterRead  bool
	Revision       int64
	Size           int64
	EditSecretHash string
	Checksum       string
	DataPath       string
	SearchText     string
}

// DiffMeta represents diff metadata returned by list and search functions.
type DiffMeta struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	Tags          []string   `json:"tags,omitempty"`
	Favorite      bool       `json:"favorite,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	BurnAfterRead bool       `json:"burnAfterRead,omitempty"`
	Revision      int64      `json:"revision,omitempty"`
	Size          int64      `json:"size,omitempty"`
}

// CachedDiff holds one diff and its search fields.
// ContentLower remains for source compatibility. Storage does not populate it.
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
	UpdatedAt      time.Time
	Tags           []string
	Favorite       bool
	ExpiresAt      *time.Time
	BurnAfterRead  bool
	Revision       int64
	Size           int64
	EditSecretHash string
	Checksum       string
	DataPath       string
	SearchText     string
}

// DiffData represents the on-disk format for one saved diff.
type DiffData struct {
	Base           string `json:"base"`
	Compare        string `json:"compare"`
	BaseContent    string `json:"baseContent"`
	CompareContent string `json:"compareContent"`
}

// RevisionInfo describes one immutable revision snapshot.
type RevisionInfo struct {
	Kind      ItemKind  `json:"kind"`
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
	Checksum  string    `json:"checksum"`
}
