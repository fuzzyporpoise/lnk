package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// ---------- Restore tests ----------

func TestRestore_CommonOnly_EmptyHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Set up tracked file but remove the live symlink so Restore has work to do.
	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(info.Restored) != 1 || info.Restored[0] != ".bashrc" {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
	if len(info.BackedUp) != 0 {
		t.Errorf("BackedUp = %v, want []", info.BackedUp)
	}

	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRestore_CommonOnly_ExplicitCommonHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "common", false)
	if err != nil {
		t.Fatalf("Restore with explicit common: %v", err)
	}

	if len(info.Restored) != 1 {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRestore_CommonPlusHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	commonStorage, commonLive := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	hostStorage, hostLive := setupTrackedFile(t, repoPath, home, "testhost", ".vimrc", "\" vimrc")

	// Remove both symlinks.
	if err := os.Remove(commonLive); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hostLive); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "testhost", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(info.Restored) != 2 {
		t.Errorf("Restored = %v, want 2 entries", info.Restored)
	}

	testhelpers.AssertSymlink(t, commonLive, commonStorage)
	testhelpers.AssertSymlink(t, hostLive, hostStorage)
}

func TestRestore_Idempotent(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Symlink is already correct — Restore should skip it.
	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(info.Restored) != 0 {
		t.Errorf("Restored = %v, want [] for already-correct symlink", info.Restored)
	}

	// Symlink should still point to the right place.
	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRestore_SkipsMissingStorageFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Set up a tracked entry but remove the storage file to simulate a missing file.
	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Nothing should be restored — the storage file is gone.
	if len(info.Restored) != 0 {
		t.Errorf("Restored = %v, want [] for missing storage file", info.Restored)
	}
	if testhelpers.FileExists(t, livePath) {
		t.Error("expected live path to remain absent when storage file is missing")
	}
}

func TestRestore_BacksUpExistingRegularFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Replace the symlink with a regular file to simulate a conflict.
	livePath := filepath.Join(home, ".bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, livePath, "# local version")

	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(info.BackedUp) != 1 || info.BackedUp[0] != ".bashrc" {
		t.Errorf("BackedUp = %v, want [.bashrc]", info.BackedUp)
	}

	// Backup file should exist with the original local content.
	backupPath := livePath + ".lnk-backup"
	if !testhelpers.FileExists(t, backupPath) {
		t.Error("expected backup file to exist")
	}
	content, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# local version" {
		t.Errorf("backup content = %q, want %q", string(content), "# local version")
	}

	// Live path should now be the managed symlink.
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRestore_BackupPathAlreadyExists(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Replace symlink with regular file and pre-create the backup path.
	livePath := filepath.Join(home, ".bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, livePath, "# local version")
	testhelpers.MakeFile(t, livePath+".lnk-backup", "# existing backup")

	_, err := svc.Restore(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error when backup path already exists, got nil")
	}
	if !errors.Is(err, lnkerror.ErrBackupExists) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrBackupExists)
	}

	// Existing backup should be untouched.
	content, _ := os.ReadFile(livePath + ".lnk-backup")
	if string(content) != "# existing backup" {
		t.Error("existing backup file should not have been modified")
	}
}

func TestRestore_ReplacesStaleSymlink(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Replace the correct symlink with a stale one pointing nowhere.
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/path", livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(info.Restored) != 1 {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
	// Stale symlink should be replaced with the correct one.
	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRestore_DryRun(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Remove symlink so there is work to report.
	livePath := filepath.Join(home, ".bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "", true)
	if err != nil {
		t.Fatalf("Restore dry run: %v", err)
	}

	// Should report the file as would-be restored.
	if len(info.Restored) != 1 || info.Restored[0] != ".bashrc" {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}

	// Live path must not have been created.
	if testhelpers.FileExists(t, livePath) {
		t.Error("dry run must not create live symlink")
	}

	// Storage file must be untouched.
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	if !testhelpers.FileExists(t, storagePath) {
		t.Error("dry run must not remove storage file")
	}

	// No backup must have been created.
	if testhelpers.FileExists(t, livePath+".lnk-backup") {
		t.Error("dry run must not create backup file")
	}
}

func TestRestore_DryRun_WithConflict(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Replace symlink with a regular file to create a conflict.
	livePath := filepath.Join(home, ".bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, livePath, "# local version")

	info, err := svc.Restore(context.Background(), "", true)
	if err != nil {
		t.Fatalf("Restore dry run with conflict: %v", err)
	}

	if len(info.BackedUp) != 1 || info.BackedUp[0] != ".bashrc" {
		t.Errorf("BackedUp = %v, want [.bashrc]", info.BackedUp)
	}

	// Regular file must still be in place — no backup created, no symlink created.
	backupPath := livePath + ".lnk-backup"
	if testhelpers.FileExists(t, backupPath) {
		t.Error("dry run must not create backup file")
	}
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	if !testhelpers.FileExists(t, storagePath) {
		t.Error("dry run must not remove storage file")
	}
	// Live path should still be the regular file, not a symlink.
	liveInfo, err := os.Lstat(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if liveInfo.Mode()&os.ModeSymlink != 0 {
		t.Error("dry run must not replace regular file with symlink")
	}
}

func TestRestore_BlockedByCollision(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Write the same path into two scope trackers to create a collision.
	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Manually add the same path to a host tracker without going through Add.
	hostTrackerPath := filepath.Join(repoPath, ".lnk.testhost")
	if err := os.WriteFile(hostTrackerPath, []byte(".bashrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Restore(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error when ownership collision exists, got nil")
	}
	if !errors.Is(err, lnkerror.ErrDuplicateOwnership) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrDuplicateOwnership)
	}
}

func TestRestore_BlockedByCollision_DryRun(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	hostTrackerPath := filepath.Join(repoPath, ".lnk.testhost")
	if err := os.WriteFile(hostTrackerPath, []byte(".bashrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Collision scan happens before any filesystem changes, so dry-run still errors.
	_, err := svc.Restore(context.Background(), "", true)
	if err == nil {
		t.Fatal("expected error when ownership collision exists in dry run, got nil")
	}
	if !errors.Is(err, lnkerror.ErrDuplicateOwnership) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrDuplicateOwnership)
	}
}

func TestRestore_DryRun_HostScope(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".vimrc", "\" vimrc")

	livePath := filepath.Join(home, ".vimrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "testhost", true)
	if err != nil {
		t.Fatalf("Restore dry run with host: %v", err)
	}

	if len(info.Restored) != 1 || info.Restored[0] != ".vimrc" {
		t.Errorf("Restored = %v, want [.vimrc]", info.Restored)
	}
	if testhelpers.FileExists(t, livePath) {
		t.Error("dry run must not create live symlink for host scope")
	}
}

func TestRestore_V1_CommonScope(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore v1: %v", err)
	}

	if len(info.Restored) != 1 {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRestore_V1Legacy_CommonScope(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("Restore v1 legacy: %v", err)
	}

	if len(info.Restored) != 1 {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
	testhelpers.AssertSymlink(t, livePath, storagePath)
}
