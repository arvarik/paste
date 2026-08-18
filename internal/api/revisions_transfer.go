package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

const itemExportSchemaVersion = 1

type itemExport struct {
	SchemaVersion  int              `json:"schemaVersion"`
	Kind           models.ItemKind  `json:"kind"`
	Title          string           `json:"title"`
	Language       string           `json:"language,omitempty"`
	Content        string           `json:"content,omitempty"`
	Diff           *models.DiffData `json:"diff,omitempty"`
	Tags           []string         `json:"tags,omitempty"`
	Favorite       bool             `json:"favorite,omitempty"`
	ExpiresAt      *time.Time       `json:"expiresAt,omitempty"`
	BurnAfterRead  bool             `json:"burnAfterRead,omitempty"`
	SourceID       string           `json:"sourceId,omitempty"`
	SourceRevision int64            `json:"sourceRevision,omitempty"`
	ExportedAt     time.Time        `json:"exportedAt"`
}

type publicRevisionMetadata struct {
	SchemaVersion int             `json:"schemaVersion"`
	Kind          models.ItemKind `json:"kind"`
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Language      string          `json:"language,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	Tags          []string        `json:"tags"`
	Favorite      bool            `json:"favorite"`
	ExpiresAt     *time.Time      `json:"expiresAt,omitempty"`
	BurnAfterRead bool            `json:"burnAfterRead"`
	Revision      int64           `json:"revision"`
	Size          int64           `json:"size"`
	Checksum      string          `json:"checksum"`
}

type publicRevision struct {
	Metadata     publicRevisionMetadata `json:"metadata"`
	PasteContent string                 `json:"pasteContent,omitempty"`
	Diff         *models.DiffData       `json:"diff,omitempty"`
}

type publicRevisionInfo struct {
	Kind      models.ItemKind `json:"kind"`
	ID        string          `json:"id"`
	Revision  int64           `json:"revision"`
	CreatedAt time.Time       `json:"createdAt"`
	Size      int64           `json:"size"`
}

func revisionForResponse(document storage.Revision) publicRevision {
	metadata := document.Metadata
	return publicRevision{
		Metadata: publicRevisionMetadata{
			SchemaVersion: metadata.SchemaVersion, Kind: metadata.Kind, ID: metadata.ID,
			Title: metadata.Title, Language: metadata.Language, CreatedAt: metadata.CreatedAt,
			UpdatedAt: metadata.UpdatedAt, Tags: append([]string(nil), metadata.Tags...),
			Favorite: metadata.Favorite, ExpiresAt: metadata.ExpiresAt,
			BurnAfterRead: metadata.BurnAfterRead, Revision: metadata.Revision,
			Size: metadata.Size, Checksum: metadata.Checksum,
		},
		PasteContent: document.PasteContent,
		Diff:         document.Diff,
	}
}

func parseRevisionPath(request *http.Request) (int64, error) {
	revision, err := strconv.ParseInt(request.PathValue("revision"), 10, 64)
	if err != nil || revision < 1 {
		return 0, errors.New("revision must be a positive integer")
	}
	return revision, nil
}

func availableItemMetadata(kind models.ItemKind, id string) (models.ItemMetadata, error) {
	metadata, err := storage.GetItemMetadata(kind, id)
	if err != nil {
		return models.ItemMetadata{}, err
	}
	if metadata.ExpiresAt != nil && !metadata.ExpiresAt.After(time.Now()) {
		return models.ItemMetadata{}, storage.ErrExpired
	}
	return metadata, nil
}

func listRevisions(writer http.ResponseWriter, request *http.Request, kind models.ItemKind, noun string) {
	id := request.PathValue("id")
	metadata, err := availableItemMetadata(kind, id)
	if err != nil {
		respondStorageError(writer, err, noun)
		return
	}
	revisions, err := storage.ListRevisions(kind, id)
	if err != nil {
		respondStorageError(writer, err, noun)
		return
	}
	publicRevisions := make([]publicRevisionInfo, 0, len(revisions))
	for _, revision := range revisions {
		publicRevisions = append(publicRevisions, publicRevisionInfo{
			Kind: revision.Kind, ID: revision.ID, Revision: revision.Revision,
			CreatedAt: revision.CreatedAt, Size: revision.Size,
		})
	}
	respondJSON(writer, http.StatusOK, map[string]any{
		"items": publicRevisions, "currentRevision": metadata.Revision,
	})
}

func getRevision(writer http.ResponseWriter, request *http.Request, kind models.ItemKind, noun string) {
	id := request.PathValue("id")
	revision, err := parseRevisionPath(request)
	if err != nil {
		respondJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	document, err := storage.GetRevision(kind, id, revision)
	if err != nil {
		respondStorageError(writer, err, noun+" revision")
		return
	}
	if document.Metadata.BurnAfterRead {
		writer.Header().Set("Cache-Control", "no-store")
	}
	respondJSON(writer, http.StatusOK, revisionForResponse(document))
}

func restoreRevision(writer http.ResponseWriter, request *http.Request, kind models.ItemKind, noun string) {
	id := request.PathValue("id")
	revision, err := parseRevisionPath(request)
	if err != nil {
		respondJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var body struct {
		ExpectedRevision *int64 `json:"expectedRevision"`
	}
	if request.ContentLength != 0 {
		if err := decodeJSONRequest(writer, request, &body); err != nil {
			respondJSONDecodeError(writer, err)
			return
		}
		if body.ExpectedRevision != nil && *body.ExpectedRevision < 1 {
			respondJSON(writer, http.StatusBadRequest, map[string]string{"error": "expectedRevision must be a positive integer"})
			return
		}
	}
	var committedRevision int64
	if principalCanWrite(request) {
		committedRevision, err = storage.RestoreRevisionTrustedExpected(kind, id, revision, body.ExpectedRevision)
	} else {
		committedRevision, err = storage.RestoreRevisionExpected(kind, id, revision, requestEditSecret(request), body.ExpectedRevision)
	}
	if err != nil {
		respondStorageError(writer, err, noun)
		return
	}
	respondJSON(writer, http.StatusOK, map[string]any{"id": id, "revision": committedRevision})
}

func handleListPasteRevisions(writer http.ResponseWriter, request *http.Request) {
	listRevisions(writer, request, models.ItemKindPaste, "Paste")
}

func handleGetPasteRevision(writer http.ResponseWriter, request *http.Request) {
	getRevision(writer, request, models.ItemKindPaste, "Paste")
}

func handleRestorePasteRevision(writer http.ResponseWriter, request *http.Request) {
	restoreRevision(writer, request, models.ItemKindPaste, "Paste")
}

func handleListDiffRevisions(writer http.ResponseWriter, request *http.Request) {
	listRevisions(writer, request, models.ItemKindDiff, "Diff")
}

func handleGetDiffRevision(writer http.ResponseWriter, request *http.Request) {
	getRevision(writer, request, models.ItemKindDiff, "Diff")
}

func handleRestoreDiffRevision(writer http.ResponseWriter, request *http.Request) {
	restoreRevision(writer, request, models.ItemKindDiff, "Diff")
}

func handleExportPaste(writer http.ResponseWriter, request *http.Request) {
	paste, err := storage.GetPaste(request.PathValue("id"))
	if err != nil {
		respondStorageError(writer, err, "Paste")
		return
	}
	document := itemExport{
		SchemaVersion: itemExportSchemaVersion, Kind: models.ItemKindPaste,
		Title: paste.Title, Language: paste.Language, Content: paste.Content,
		Tags: paste.Tags, Favorite: paste.Favorite, ExpiresAt: paste.ExpiresAt,
		BurnAfterRead: paste.BurnAfterRead, SourceID: paste.ID,
		SourceRevision: paste.Revision, ExportedAt: time.Now().UTC(),
	}
	writeItemExport(writer, paste.ID+".paste.json", document)
}

func handleExportDiff(writer http.ResponseWriter, request *http.Request) {
	diff, err := storage.GetDiff(request.PathValue("id"))
	if err != nil {
		respondStorageError(writer, err, "Diff")
		return
	}
	document := itemExport{
		SchemaVersion: itemExportSchemaVersion, Kind: models.ItemKindDiff,
		Title: diff.Title,
		Diff:  &models.DiffData{Base: diff.Base, Compare: diff.Compare, BaseContent: diff.BaseContent, CompareContent: diff.CompareContent},
		Tags:  diff.Tags, Favorite: diff.Favorite, ExpiresAt: diff.ExpiresAt,
		BurnAfterRead: diff.BurnAfterRead, SourceID: diff.ID,
		SourceRevision: diff.Revision, ExportedAt: time.Now().UTC(),
	}
	writeItemExport(writer, diff.ID+".diff.json", document)
}

func writeItemExport(writer http.ResponseWriter, filename string, document itemExport) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	respondJSON(writer, http.StatusOK, document)
}

func handleImportItem(writer http.ResponseWriter, request *http.Request) {
	if !requireCreatePermission(writer, request) {
		return
	}
	var document itemExport
	if err := decodeJSONRequest(writer, request, &document); err != nil {
		respondJSONDecodeError(writer, err)
		return
	}
	if document.SchemaVersion != itemExportSchemaVersion {
		respondJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "Unsupported export schema"})
		return
	}
	tags, expiresAt, err := validateItemOptions(document.Tags, document.ExpiresAt, true)
	if err != nil {
		respondJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	options := storage.CreateOptions{
		Tags: tags, Favorite: document.Favorite, ExpiresAt: expiresAt, BurnAfterRead: document.BurnAfterRead,
	}
	title := util.SanitizeTitle(strings.TrimSpace(document.Title), "Imported")
	var id string
	var editSecret string
	switch document.Kind {
	case models.ItemKindPaste:
		if strings.TrimSpace(document.Content) == "" || document.Diff != nil {
			respondJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "A paste export must contain paste content only"})
			return
		}
		id, editSecret, err = storage.CreatePasteWithOptions(title, document.Content, util.NormalizeLanguage(document.Language), options)
	case models.ItemKindDiff:
		if document.Diff == nil || strings.TrimSpace(document.Content) != "" ||
			strings.TrimSpace(document.Diff.BaseContent) == "" && strings.TrimSpace(document.Diff.CompareContent) == "" {
			respondJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "A diff export must contain diff content only"})
			return
		}
		id, editSecret, err = storage.CreateDiffWithOptions(title, document.Diff.Base, document.Diff.Compare, document.Diff.BaseContent, document.Diff.CompareContent, options)
	default:
		respondJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "Export kind must be paste or diff"})
		return
	}
	if err != nil {
		respondStorageError(writer, err, "Imported item")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	respondJSON(writer, http.StatusCreated, map[string]any{
		"id": id, "kind": document.Kind, "title": title, "editSecret": editSecret, "revision": int64(1),
	})
}
