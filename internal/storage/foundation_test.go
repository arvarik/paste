package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
)

func TestLegacyMigrationPreservesPasteAndDiff(t *testing.T) {
	setupStorageTest(t)
	createdAt := time.Now().Add(-24 * time.Hour).Round(time.Second)
	pastePath := filepath.Join(DataDir, "abc123_Legacy.py")
	if err := os.WriteFile(pastePath, []byte("print('safe')"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pastePath, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(DataDir, "diffs"), dataDirMode); err != nil {
		t.Fatal(err)
	}
	diffContent, _ := json.Marshal(models.DiffData{Base: "a", Compare: "b", BaseContent: "left", CompareContent: "right"})
	diffPath := filepath.Join(DataDir, "diffs", "def456_Legacy.json")
	if err := os.WriteFile(diffPath, diffContent, dataFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(diffPath, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}

	LoadCacheFromDisk()
	LoadDiffCacheFromDisk()
	paste, err := GetPaste("abc123")
	if err != nil || paste.Content != "print('safe')" || paste.Language != "python" {
		t.Fatalf("migrated paste = %#v, %v", paste, err)
	}
	diff, err := GetDiff("def456")
	if err != nil || diff.BaseContent != "left" {
		t.Fatalf("migrated diff = %#v, %v", diff, err)
	}
	for _, item := range []struct {
		kind models.ItemKind
		id   string
	}{{models.ItemKindPaste, "abc123"}, {models.ItemKindDiff, "def456"}} {
		metadata, err := GetItemMetadata(item.kind, item.id)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Revision != 1 || metadata.Size == 0 || metadata.Checksum == "" || !metadata.CreatedAt.Equal(createdAt) {
			t.Fatalf("metadata = %#v", metadata)
		}
		if metadata.EditSecretHash != "" || metadata.Tags == nil || metadata.Favorite || metadata.ExpiresAt != nil || metadata.BurnAfterRead {
			t.Fatalf("legacy defaults = %#v", metadata)
		}
	}
	if _, err := os.Stat(pastePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy paste still exists: %v", err)
	}
	if _, err := os.Stat(diffPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy diff still exists: %v", err)
	}
}

func TestLegacyMigrationPreservesSourceOnTargetCollision(t *testing.T) {
	setupStorageTest(t)
	legacyPath := filepath.Join(DataDir, "abc123_Legacy.txt")
	if err := os.WriteFile(legacyPath, []byte("correct"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	metadata := models.ItemMetadata{Kind: models.ItemKindPaste, ID: "abc123", DataFile: "content-r1.txt"}
	if err := os.MkdirAll(itemDirectory(models.ItemKindPaste, "abc123"), dataDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(itemDataPath(metadata), []byte("different"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyPastes(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("migrateLegacyPastes() error = %v, want corruption", err)
	}
	if content, err := os.ReadFile(legacyPath); err != nil || string(content) != "correct" {
		t.Fatalf("legacy source = %q, %v", content, err)
	}
	if _, err := os.Stat(itemMetadataPath(models.ItemKindPaste, "abc123")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("collision wrote metadata: %v", err)
	}
}

func TestEditSecretAndOptimisticRevision(t *testing.T) {
	setupStorageTest(t)
	id, secret, err := CreatePasteWithOptions("Secured", "one", "text", CreateOptions{Tags: []string{"work"}, Favorite: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 32 || secret == "" {
		t.Fatalf("id=%q secret=%q", id, secret)
	}
	if ok, err := VerifyEditSecret(models.ItemKindPaste, id, secret); err != nil || !ok {
		t.Fatalf("verify = %v, %v", ok, err)
	}
	if err := UpdatePasteAuthorized(id, "Secured", "two", "text", "wrong", MetadataPatch{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong secret error = %v", err)
	}
	expected := int64(1)
	if err := UpdatePasteAuthorized(id, "Secured", "two", "text", secret, MetadataPatch{ExpectedRevision: &expected}); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteAuthorized(id, "Secured", "three", "text", secret, MetadataPatch{ExpectedRevision: &expected}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	metadata, _ := GetItemMetadata(models.ItemKindPaste, id)
	if metadata.Revision != 2 || !metadata.Favorite || len(metadata.Tags) != 1 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLegacyAuthorizedWriteRequiresSecret(t *testing.T) {
	setupStorageTest(t)
	if err := os.WriteFile(filepath.Join(DataDir, "abc123_Legacy.txt"), []byte("old"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	LoadCacheFromDisk()
	if err := UpdatePasteAuthorized("abc123", "Legacy", "new", "text", "", MetadataPatch{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v", err)
	}
	if err := UpdatePasteTrusted("abc123", "Legacy", "new", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaRejectsWithoutCreatingData(t *testing.T) {
	setupStorageTest(t)
	SetStorageLimits(StorageLimits{MaxTotalBytes: 5, MaxItemBytes: 5, MaxItems: 1, MaxSearchResults: 10, MaxSearchIndexBytes: 1024, MaxCachedContentBytes: 1024, MaxBackupBytes: 1024})
	if _, _, err := CreatePasteWithOptions("Too large", "123456", "text", CreateOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("large item error = %v", err)
	}
	id, _, err := CreatePasteWithOptions("Fits", "12345", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateDiffWithOptions("Extra", "", "", "", "", CreateOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("item count error = %v", err)
	}
	if _, err := GetPaste(id); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaIncludesRevisionStorage(t *testing.T) {
	setupStorageTest(t)
	id, secret, err := CreatePasteWithOptions("Revision quota", "1234", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	limits := GetStorageLimits()
	limits.MaxTotalBytes = 5
	SetStorageLimits(limits)
	if err := UpdatePasteAuthorized(id, "Revision quota", "5678", "text", secret, MetadataPatch{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("revision update error = %v, want quota error", err)
	}
	paste, err := GetPaste(id)
	if err != nil || paste.Content != "1234" || paste.Revision != 1 {
		t.Fatalf("paste after revision quota = %#v, %v", paste, err)
	}
}

func TestExpiryAndBurnAfterRead(t *testing.T) {
	setupStorageTest(t)
	past := time.Now().Add(-time.Minute)
	expiredID, _, err := CreatePasteWithOptions("Expired", "gone", "text", CreateOptions{ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GetPaste(expiredID); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired read error = %v", err)
	}
	if _, err := GetItemMetadata(models.ItemKindPaste, expiredID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired metadata error = %v", err)
	}

	burnID, _, err := CreatePasteWithOptions("Burn", "once", "text", CreateOptions{BurnAfterRead: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := GetPaste(burnID)
	if err != nil || first.Content != "once" {
		t.Fatalf("first burn read = %#v, %v", first, err)
	}
	if _, err := GetPaste(burnID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second burn read error = %v", err)
	}
}

func TestExpiredItemsRejectUpdatesAndRestore(t *testing.T) {
	setupStorageTest(t)
	id, secret, err := CreatePasteWithOptions("Expires", "one", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteAuthorized(id, "Expires", "two", "text", secret, MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	expiry := &past
	if err := UpdateItemMetadataTrusted(models.ItemKindPaste, id, MetadataPatch{ExpiresAt: &expiry}); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Expires", "three", "text", MetadataPatch{}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired trusted update = %v", err)
	}
	if err := UpdatePasteAuthorized(id, "Expires", "three", "text", secret, MetadataPatch{}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired secret update = %v", err)
	}
	if err := RestoreRevisionTrusted(models.ItemKindPaste, id, 1); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired restore = %v", err)
	}
}

func TestBurnItemsHidePreviewsAndRevisionReadConsumesItem(t *testing.T) {
	setupStorageTest(t)
	id, secret, err := CreatePasteWithOptions("Burn", "secret first", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	burn := true
	if err := UpdatePasteAuthorized(id, "Burn", "secret second", "text", secret, MetadataPatch{BurnAfterRead: &burn}); err != nil {
		t.Fatal(err)
	}
	page, err := ListPastesPage("", 10)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("burn list page = %#v, %v", page, err)
	}
	search, err := QueryPastesPage("secret", ItemFilter{}, "", 10)
	if err != nil || len(search.Items) != 0 {
		t.Fatalf("burn content appeared in search = %#v, %v", search, err)
	}
	revision, err := GetRevision(models.ItemKindPaste, id, 1)
	if err != nil || revision.PasteContent != "secret first" {
		t.Fatalf("burn revision = %#v, %v", revision, err)
	}
	if _, err := GetItemMetadata(models.ItemKindPaste, id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("burn revision did not consume item: %v", err)
	}
}

func TestRevisionsAndRestore(t *testing.T) {
	setupStorageTest(t)
	id, secret, err := CreateDiffWithOptions("Diff", "a", "b", "one", "two", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateDiffAuthorized(id, "Diff 2", "a", "b", "three", "four", secret, MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	revisions, err := ListRevisions(models.ItemKindDiff, id)
	if err != nil || len(revisions) != 1 || revisions[0].Revision != 1 {
		t.Fatalf("revisions = %#v, %v", revisions, err)
	}
	revision, err := GetRevision(models.ItemKindDiff, id, 1)
	if err != nil || revision.Diff == nil || revision.Diff.BaseContent != "one" {
		t.Fatalf("revision = %#v, %v", revision, err)
	}
	if err := RestoreRevision(models.ItemKindDiff, id, 1, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong restore error = %v", err)
	}
	if err := RestoreRevision(models.ItemKindDiff, id, 1, secret); err != nil {
		t.Fatal(err)
	}
	restored, err := GetDiff(id)
	if err != nil || restored.BaseContent != "one" || restored.Revision != 3 {
		t.Fatalf("restored = %#v, %v", restored, err)
	}
	if err := DeleteDiffTrusted(id); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRevisions(models.ItemKindDiff, id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted item revisions error = %v", err)
	}
}

func TestPaginationContinuesAfterCursorItemDeletion(t *testing.T) {
	setupStorageTest(t)
	SetStorageLimits(StorageLimits{MaxTotalBytes: 1 << 20, MaxItemBytes: 1 << 20, MaxItems: 20, MaxSearchResults: 20, MaxSearchIndexBytes: 1 << 20, MaxCachedContentBytes: 1 << 20, MaxBackupBytes: 1 << 20})
	for index := 0; index < 5; index++ {
		if _, err := CreatePaste(fmt.Sprintf("Paste %d", index), "common", "text"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	first, err := QueryPastesPage("common", ItemFilter{}, "", 2)
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	if err := DeletePaste(first.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	second, err := QueryPastesPage("common", ItemFilter{}, first.NextCursor, 2)
	if err != nil || len(second.Items) != 2 {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	seen := map[string]bool{}
	for _, item := range first.Items {
		seen[item.ID] = true
	}
	for _, item := range second.Items {
		if seen[item.ID] {
			t.Fatalf("duplicate item %s", item.ID)
		}
	}
}

func TestBackupRestoreAndCorruption(t *testing.T) {
	setupStorageTest(t)
	pasteID, _, err := CreatePasteWithOptions("Backup", "content", "text", CreateOptions{Tags: []string{"saved"}})
	if err != nil {
		t.Fatal(err)
	}
	diffID, _, err := CreateDiffWithOptions("Diff", "", "", "left", "right", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatal(err)
	}
	if err := DeletePaste(pasteID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteDiff(diffID); err != nil {
		t.Fatal(err)
	}
	if err := ImportBackup(bytes.NewReader(archive.Bytes()), false); err != nil {
		t.Fatal(err)
	}
	if _, err := GetPaste(pasteID); err != nil {
		t.Fatal(err)
	}
	if err := ImportBackup(bytes.NewReader(archive.Bytes()), false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate import error = %v", err)
	}
	if err := RestoreBackup(bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := GetPaste(pasteID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDiff(diffID); err != nil {
		t.Fatal(err)
	}
	report := VerifyIntegrity()
	if !report.Healthy || report.Items != 2 {
		t.Fatalf("integrity = %#v", report)
	}
	metadata, _ := GetItemMetadata(models.ItemKindPaste, pasteID)
	if err := os.WriteFile(itemDataPath(metadata), []byte("corrupt"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	report = VerifyIntegrity()
	if report.Healthy || len(report.Issues) == 0 {
		t.Fatalf("corrupt integrity = %#v", report)
	}
}

func TestExportRejectsCorruptStorage(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Corrupt", "original", "text")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := GetItemMetadata(models.ItemKindPaste, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(itemDataPath(metadata), []byte("changed"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ExportBackup() error = %v, want corruption", err)
	}
	if archive.Len() != 0 {
		t.Fatalf("corrupt export wrote %d bytes", archive.Len())
	}
}

func TestBackupLimitMatchesImportAccounting(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Limit", "content", "text")
	if err != nil {
		t.Fatal(err)
	}
	var initial bytes.Buffer
	if err := ExportBackup(&initial); err != nil {
		t.Fatal(err)
	}
	limits := GetStorageLimits()
	limits.MaxBackupBytes = int64(initial.Len())
	SetStorageLimits(limits)
	var exact bytes.Buffer
	if err := ExportBackup(&exact); err != nil {
		t.Fatalf("exact-size ExportBackup() error = %v", err)
	}
	limits.MaxBackupBytes = int64(exact.Len()) - 1
	SetStorageLimits(limits)
	if err := ExportBackup(&bytes.Buffer{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("oversize ExportBackup() error = %v, want quota error", err)
	}
	if err := ImportBackup(bytes.NewReader(exact.Bytes()), true); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("oversize ImportBackup() error = %v, want quota error", err)
	}
	limits.MaxBackupBytes = int64(exact.Len())
	SetStorageLimits(limits)
	if err := DeletePaste(id); err != nil {
		t.Fatal(err)
	}
	if err := ImportBackup(bytes.NewReader(exact.Bytes()), false); err != nil {
		t.Fatalf("same-limit ImportBackup() error = %v", err)
	}
}

func TestPurgeExpiredReleasesQuota(t *testing.T) {
	setupStorageTest(t)
	SetStorageLimits(StorageLimits{MaxTotalBytes: 4, MaxItemBytes: 4, MaxItems: 1, MaxSearchResults: 10, MaxSearchIndexBytes: 1024, MaxCachedContentBytes: 1024, MaxBackupBytes: 4096})
	past := time.Now().Add(-time.Minute)
	if _, _, err := CreatePasteWithOptions("Expired", "1234", "text", CreateOptions{ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if removed, err := PurgeExpired(time.Now()); err != nil || removed != 1 {
		t.Fatalf("PurgeExpired() = %d, %v", removed, err)
	}
	if _, _, err := CreatePasteWithOptions("Replacement", "5678", "text", CreateOptions{}); err != nil {
		t.Fatalf("replacement create = %v", err)
	}
}

func TestIntegrityRejectsMisplacedRevision(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Revision", "one", "text")
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Revision", "two", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	otherID, err := CreatePaste("Other", "value", "text")
	if err != nil {
		t.Fatal(err)
	}
	targetDir := revisionRoot(models.ItemKindPaste, otherID)
	if err := os.MkdirAll(targetDir, dataDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(revisionPath(models.ItemKindPaste, id, 1), revisionPath(models.ItemKindPaste, otherID, 1)); err != nil {
		t.Fatal(err)
	}
	if report := VerifyIntegrity(); report.Healthy {
		t.Fatalf("misplaced revision passed integrity: %#v", report)
	}
}

func TestInitializeRecoversInterruptedSwapAndDelete(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Recover", "value", "text")
	if err != nil {
		t.Fatal(err)
	}

	stamp, _ := generateStorageID()
	if err := writeStorageJournal(swapJournalName, swapJournal{Stamp: stamp}); err != nil {
		t.Fatal(err)
	}
	itemsBackup := filepath.Join(DataDir, ".old-"+itemsDirectoryName+"-"+stamp)
	if err := os.Rename(filepath.Join(DataDir, itemsDirectoryName), itemsBackup); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(); err != nil {
		t.Fatalf("swap recovery = %v", err)
	}
	if _, err := GetPaste(id); err != nil {
		t.Fatalf("paste after swap recovery = %v", err)
	}

	deleteStamp, _ := generateStorageID()
	journal := deleteJournal{Kind: models.ItemKindPaste, ID: id, Stamp: deleteStamp}
	if err := writeStorageJournal(deleteJournalName, journal); err != nil {
		t.Fatal(err)
	}
	itemTombstone, _ := deletionTombstones(journal)
	if err := os.Rename(itemDirectory(models.ItemKindPaste, id), itemTombstone); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(); err != nil {
		t.Fatalf("delete recovery = %v", err)
	}
	if _, err := GetPaste(id); err != nil {
		t.Fatalf("paste after delete recovery = %v", err)
	}
}

func TestDeleteFailureRestoresContentAndRevisions(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Delete", "one", "text")
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Delete", "two", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	originalRename := renameStoragePath
	renameStoragePath = func(oldPath, newPath string) error {
		if oldPath == revisionRoot(models.ItemKindPaste, id) && strings.Contains(filepath.Base(newPath), ".deleted-revisions-") {
			return errors.New("injected revision delete failure")
		}
		return os.Rename(oldPath, newPath)
	}
	err = DeletePaste(id)
	renameStoragePath = originalRename
	if err == nil {
		t.Fatal("DeletePaste() error = nil")
	}
	paste, readErr := GetPaste(id)
	if readErr != nil || paste.Content != "two" {
		t.Fatalf("paste after failed delete = %#v, %v", paste, readErr)
	}
	if revisions, revisionErr := ListRevisions(models.ItemKindPaste, id); revisionErr != nil || len(revisions) != 1 {
		t.Fatalf("revisions after failed delete = %#v, %v", revisions, revisionErr)
	}
}

func TestBackupRestoreAndImportHonorCurrentQuotas(t *testing.T) {
	setupStorageTest(t)
	sourceID, _, err := CreatePasteWithOptions("Source", "1234", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatal(err)
	}
	if err := DeletePaste(sourceID); err != nil {
		t.Fatal(err)
	}
	destinationID, _, err := CreatePasteWithOptions("Destination", "5678", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	configured := GetStorageLimits()
	configured.MaxTotalBytes = 6
	configured.MaxItemBytes = 6
	SetStorageLimits(configured)

	if err := ImportBackup(bytes.NewReader(archive.Bytes()), false); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("ImportBackup() error = %v, want quota error", err)
	}
	if _, err := GetPaste(destinationID); err != nil {
		t.Fatalf("failed import removed current data: %v", err)
	}
	if _, err := GetPaste(sourceID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed import added source item: %v", err)
	}

	configured.MaxItemBytes = 3
	SetStorageLimits(configured)
	if err := RestoreBackup(bytes.NewReader(archive.Bytes())); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("RestoreBackup() error = %v, want quota error", err)
	}
	if _, err := GetPaste(destinationID); err != nil {
		t.Fatalf("failed restore changed current data: %v", err)
	}
}

func TestRestoreRollbackPreservesBothTrees(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Rollback", "backup", "text")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Rollback", "current", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}

	originalRename := renameStoragePath
	renameStoragePath = func(oldPath, newPath string) error {
		if newPath == filepath.Join(DataDir, revisionsDirectory) && filepath.Base(oldPath) == revisionsDirectory {
			return errors.New("injected revision-tree swap failure")
		}
		return os.Rename(oldPath, newPath)
	}
	defer func() { renameStoragePath = originalRename }()
	if err := RestoreBackup(bytes.NewReader(archive.Bytes())); err == nil {
		t.Fatal("RestoreBackup() error = nil")
	}
	paste, err := GetPaste(id)
	if err != nil || paste.Content != "current" {
		t.Fatalf("paste after rollback = %#v, %v", paste, err)
	}
	revisions, err := ListRevisions(models.ItemKindPaste, id)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("revisions after rollback = %#v, %v", revisions, err)
	}
	if report := VerifyIntegrity(); !report.Healthy {
		t.Fatalf("integrity after rollback = %#v", report)
	}
}

func TestConcurrentReadsAndUpdates(t *testing.T) {
	setupStorageTest(t)
	id, _, err := CreatePasteWithOptions("Concurrent", "start", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for iteration := 0; iteration < 20; iteration++ {
				if worker%2 == 0 {
					_, _ = GetPaste(id)
				} else {
					_ = UpdatePasteTrusted(id, "Concurrent", fmt.Sprintf("%d-%d", worker, iteration), "text", MetadataPatch{})
				}
			}
		}(worker)
	}
	workers.Wait()
	if _, err := GetPaste(id); err != nil {
		t.Fatal(err)
	}
	if report := VerifyIntegrity(); !report.Healthy {
		t.Fatalf("integrity = %#v", report)
	}
}

func TestContentAndSearchCachesStayWithinBounds(t *testing.T) {
	setupStorageTest(t)
	SetStorageLimits(StorageLimits{MaxTotalBytes: 1 << 20, MaxItemBytes: 1 << 20, MaxItems: 20, MaxSearchResults: 20, MaxSearchIndexBytes: 32, MaxCachedContentBytes: 8, MaxBackupBytes: 1 << 20})
	for index := 0; index < 4; index++ {
		if _, err := CreatePaste(fmt.Sprintf("Cache %d", index), "12345678", "text"); err != nil {
			t.Fatal(err)
		}
	}
	contentCacheMu.Lock()
	contentBytes, indexBytes := contentCacheUsed, searchIndexUsed
	contentCacheMu.Unlock()
	if contentBytes > 8 {
		t.Fatalf("content cache uses %d bytes", contentBytes)
	}
	if indexBytes > 32 {
		t.Fatalf("search index uses %d bytes", indexBytes)
	}
	for _, item := range ListPastes() {
		paste, err := GetPaste(item.ID)
		if err != nil || paste.Content != "12345678" {
			t.Fatalf("uncached read = %#v, %v", paste, err)
		}
	}
}

func TestUncachedReadDetectsCorruptContent(t *testing.T) {
	setupStorageTest(t)
	id, _, err := CreatePasteWithOptions("Corrupt", "valid", "text", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	GlobalCache.Lock()
	cached := GlobalCache.Items[id]
	cached.Content = ""
	GlobalCache.Items[id] = cached
	GlobalCache.Unlock()
	if err := os.WriteFile(cached.DataPath, []byte("wrong"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := GetPaste(id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("GetPaste() error = %v, want corruption", err)
	}
}
