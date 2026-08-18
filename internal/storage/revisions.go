package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

type revisionDocument struct {
	Metadata models.ItemMetadata `json:"metadata"`
	Data     []byte              `json:"data"`
}

// Revision contains decoded content for one immutable snapshot.
type Revision struct {
	Metadata     models.ItemMetadata `json:"metadata"`
	PasteContent string              `json:"pasteContent,omitempty"`
	Diff         *models.DiffData    `json:"diff,omitempty"`
}

func snapshotRevision(metadata models.ItemMetadata, currentSizeDelta int64) error {
	data, err := readStorageFile(itemDataPath(metadata), metadata.Size)
	if err != nil {
		return err
	}
	revisionMetadata := metadata
	revisionMetadata.EditSecretHash = ""
	document := revisionDocument{Metadata: revisionMetadata, Data: data}
	content, err := json.Marshal(document)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	path := revisionPath(metadata.Kind, metadata.ID, metadata.Revision)
	if _, err := os.Lstat(path); err == nil {
		existing, readErr := readRevision(metadata.Kind, metadata.ID, metadata.Revision)
		if readErr != nil {
			return readErr
		}
		if existing.Metadata.Checksum != metadata.Checksum || checksumBytes(existing.Data) != metadata.Checksum {
			return fmt.Errorf("%w: revision %d already has different content", ErrCorrupt, metadata.Revision)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := checkAdditionalStorage(currentSizeDelta + int64(len(content))); err != nil {
		return err
	}
	if err := reconcileCommittedFile(path, content, createFileExclusive(path, content)); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		existing, readErr := readRevision(metadata.Kind, metadata.ID, metadata.Revision)
		if readErr != nil {
			return readErr
		}
		if existing.Metadata.Checksum != metadata.Checksum || checksumBytes(existing.Data) != metadata.Checksum {
			return fmt.Errorf("%w: revision %d already has different content", ErrCorrupt, metadata.Revision)
		}
	} else {
		adjustUsage(0, int64(len(content)), 0)
	}
	return nil
}

// ListRevisions returns immutable snapshots in ascending revision order.
func ListRevisions(kind models.ItemKind, id string) ([]models.RevisionInfo, error) {
	if !validStorageID(id) {
		return nil, fs.ErrNotExist
	}
	if _, err := readMetadata(kind, id); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(revisionRoot(kind, id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []models.RevisionInfo{}, nil
		}
		return nil, err
	}
	result := make([]models.RevisionInfo, 0, len(entries))
	for _, entry := range entries {
		revision, valid := revisionNumberFromName(entry.Name())
		if !valid || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		document, err := readRevision(kind, id, revision)
		if err != nil {
			return nil, err
		}
		result = append(result, models.RevisionInfo{
			Kind:      kind,
			ID:        id,
			Revision:  revision,
			CreatedAt: document.Metadata.UpdatedAt,
			Size:      int64(len(document.Data)),
			Checksum:  checksumBytes(document.Data),
		})
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Revision < result[right].Revision
	})
	return result, nil
}

func readRevision(kind models.ItemKind, id string, revision int64) (revisionDocument, error) {
	configured := GetStorageLimits().MaxItemBytes
	if configured <= 0 || configured > 1<<62 {
		configured = 1 << 30
	}
	content, err := readStorageFile(revisionPath(kind, id, revision), configured*2+(1<<20))
	if err != nil {
		return revisionDocument{}, err
	}
	var document revisionDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return revisionDocument{}, fmt.Errorf("%w: decode revision: %v", ErrCorrupt, err)
	}
	if document.Metadata.Kind != kind || document.Metadata.ID != id || document.Metadata.Revision != revision {
		return revisionDocument{}, fmt.Errorf("%w: revision path mismatch", ErrCorrupt)
	}
	if checksumBytes(document.Data) != document.Metadata.Checksum {
		return revisionDocument{}, fmt.Errorf("%w: revision checksum mismatch", ErrCorrupt)
	}
	if int64(len(document.Data)) != document.Metadata.Size {
		return revisionDocument{}, fmt.Errorf("%w: revision size mismatch", ErrCorrupt)
	}
	return document, nil
}

// GetRevision returns one decoded revision and applies expiry and burn rules.
func GetRevision(kind models.ItemKind, id string, revision int64) (Revision, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(kind, id)
	if err != nil {
		return Revision{}, err
	}
	if isExpired(metadata, time.Now()) {
		return Revision{}, ErrExpired
	}
	document, err := readRevision(kind, id, revision)
	if err != nil {
		return Revision{}, err
	}
	result := Revision{Metadata: document.Metadata}
	if kind == models.ItemKindPaste {
		result.PasteContent = string(document.Data)
	} else {
		var data models.DiffData
		if err := json.Unmarshal(document.Data, &data); err != nil {
			return Revision{}, fmt.Errorf("%w: decode diff revision: %v", ErrCorrupt, err)
		}
		result.Diff = &data
	}
	if metadata.BurnAfterRead {
		if err := deleteItemLocked(kind, id); err != nil {
			return Revision{}, err
		}
	}
	return result, nil
}

// RestoreRevision restores a snapshot as a new revision.
func RestoreRevision(kind models.ItemKind, id string, revision int64, editSecret string) error {
	_, err := restoreRevision(kind, id, revision, editSecret, nil, true)
	return err
}

// RestoreRevisionWithRevision restores a snapshot and returns the committed revision.
func RestoreRevisionWithRevision(kind models.ItemKind, id string, revision int64, editSecret string) (int64, error) {
	return restoreRevision(kind, id, revision, editSecret, nil, true)
}

// RestoreRevisionExpected restores a snapshot when the current revision matches.
func RestoreRevisionExpected(kind models.ItemKind, id string, revision int64, editSecret string, expectedRevision *int64) (int64, error) {
	return restoreRevision(kind, id, revision, editSecret, expectedRevision, true)
}

// RestoreRevisionTrusted restores a snapshot for an authenticated admin caller.
func RestoreRevisionTrusted(kind models.ItemKind, id string, revision int64) error {
	_, err := restoreRevision(kind, id, revision, "", nil, false)
	return err
}

// RestoreRevisionTrustedWithRevision restores a snapshot and returns the committed revision.
func RestoreRevisionTrustedWithRevision(kind models.ItemKind, id string, revision int64) (int64, error) {
	return restoreRevision(kind, id, revision, "", nil, false)
}

// RestoreRevisionTrustedExpected restores a snapshot for an admin after a revision check.
func RestoreRevisionTrustedExpected(kind models.ItemKind, id string, revision int64, expectedRevision *int64) (int64, error) {
	return restoreRevision(kind, id, revision, "", expectedRevision, false)
}

func restoreRevision(kind models.ItemKind, id string, revision int64, editSecret string, expectedRevision *int64, requireSecret bool) (int64, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	current, err := readMetadata(kind, id)
	if err != nil {
		return 0, err
	}
	if isExpired(current, time.Now()) {
		return 0, ErrExpired
	}
	if requireSecret && !verifySecretHash(current.EditSecretHash, editSecret) {
		return 0, ErrUnauthorized
	}
	if expectedRevision != nil && current.Revision != *expectedRevision {
		return 0, ErrConflict
	}
	document, err := readRevision(kind, id, revision)
	if err != nil {
		return 0, err
	}
	var diffData models.DiffData
	if kind == models.ItemKindDiff {
		if err := json.Unmarshal(document.Data, &diffData); err != nil {
			return 0, fmt.Errorf("%w: decode diff revision: %v", ErrCorrupt, err)
		}
	}
	if err := checkQuota(current.Size, int64(len(document.Data)), false); err != nil {
		return 0, err
	}
	if err := snapshotRevision(current, int64(len(document.Data))-current.Size); err != nil {
		return 0, err
	}

	restored := document.Metadata
	restored.CreatedAt = current.CreatedAt
	restored.UpdatedAt = time.Now().UTC()
	restored.EditSecretHash = current.EditSecretHash
	restored.Revision = current.Revision + 1
	restored.Size = int64(len(document.Data))
	restored.Checksum = checksumBytes(document.Data)
	extension := filepath.Ext(current.DataFile)
	if kind == models.ItemKindPaste {
		extension = util.LangToExt(restored.Language)
	}
	restored.DataFile = fmt.Sprintf("content-r%d%s", restored.Revision, extension)
	if err := reconcileCommittedFile(itemDataPath(restored), document.Data, createFileExclusive(itemDataPath(restored), document.Data)); err != nil {
		return 0, err
	}
	cleanup, err := beginContentCleanup(current, restored)
	if err != nil {
		_ = removeFileDurably(itemDataPath(restored))
		return 0, err
	}
	if err := writeMetadata(restored); err != nil {
		return 0, errors.Join(err, recoverContentCleanup())
	}
	adjustUsage(current.Size, restored.Size, 0)
	if kind == models.ItemKindPaste {
		storePasteCache(cachedPasteFromData(restored, document.Data))
	} else {
		storeDiffCache(cachedDiffFromData(restored, diffData))
	}
	_ = finishContentCleanup(cleanup)
	return restored.Revision, nil
}
