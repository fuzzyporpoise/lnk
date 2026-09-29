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

// ---------- Doctor scan tests ----------

func TestDoctor_Clean(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if report.HasIssues() {
		t.Errorf("expected no issues on clean repo, got: marker=%v collisions=%d scopes=%d",
			report.MarkerMissing, len(report.Collisions), len(report.ScopeResults))
	}
	if report.MarkerMissing {
		t.Error("expected MarkerMissing=false")
	}
	if len(report.Collisions) != 0 {
		t.Errorf("expected no collisions, got %d", len(report.Collisions))
	}
}

func TestDoctor_MarkerMissing(t *testing.T) {
	svc, _ := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	// Remove the .lnk file so FindVersion has nothing to detect — but we still
	// need a git repo so requireGitRepo passes. Write a v2 tracker so the
	// service can determine the scope, then remove the marker.
	markerPath := filepath.Join(repoPath, ".lnkrepo")
	os.Remove(markerPath)

	// Write a v2 tracker so format detection can fall back to .lnk.
	// TestHomeV1Legacy already has .lnk written, so marker is just absent.
	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !report.MarkerMissing {
		t.Error("expected MarkerMissing=true")
	}
	if !report.HasIssues() {
		t.Error("expected HasIssues=true when marker missing")
	}
}

func TestDoctor_InvalidEntry(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Set up a tracked file then delete it from storage to create an invalid entry.
	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with missing storage file")
	}

	found := false
	for _, result := range report.ScopeResults {
		if result.HasIssues() {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one ScopeResult with issues")
	}
}

func TestDoctor_BrokenSymlink_SymlinkRemoved(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	_, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Remove the live symlink to simulate accidental deletion.
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with missing symlink")
	}

	found := false
	for _, result := range report.ScopeResults {
		if result.HasIssues() {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ScopeResult with broken label and issues")
	}
}

func TestDoctor_BrokenSymlink_ConflictingFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	_, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Replace the managed symlink with a regular file to simulate a conflict.
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, livePath, "# local conflicting version")

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with conflicting file at live path")
	}
}

func TestDoctor_OwnershipCollision(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Write the same path into a host tracker to create a collision.
	hostTracker := filepath.Join(repoPath, ".lnk.testhost")
	if err := os.WriteFile(hostTracker, []byte(".bashrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if len(report.Collisions) != 1 {
		t.Fatalf("expected 1 collision, got %d", len(report.Collisions))
	}
	if report.Collisions[0].Path != ".bashrc" {
		t.Errorf("collision path = %q, want .bashrc", report.Collisions[0].Path)
	}
	if len(report.Collisions[0].Scopes) != 2 {
		t.Errorf("collision scopes = %v, want 2 scopes", report.Collisions[0].Scopes)
	}
	if !report.HasIssues() {
		t.Error("expected HasIssues=true with collision")
	}
}

func TestDoctor_AllScope_ScansAllScopesInOrder(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	setupTrackedFile(t, repoPath, home, "zhost", ".zshrc", "# zsh")
	setupTrackedFile(t, repoPath, home, "ahost", ".vimrc", "\" vim")

	report, err := svc.Doctor(context.Background(), "", true, false, false)
	if err != nil {
		t.Fatalf("Doctor --all: %v", err)
	}

	// ScopeResults contains one result per scope — with --all, broken symlink
	// check is skipped. Expect common, ahost, zhost in that order (common
	// first, hosts alphabetical).
	var scopeNames []string
	for _, result := range report.ScopeResults {
		scopeNames = append(scopeNames, result.Name)
	}

	if len(scopeNames) < 3 {
		t.Fatalf("expected at least 3 scope results, got %d: %v", len(scopeNames), scopeNames)
	}
	if scopeNames[0] != "common" {
		t.Errorf("scopeNames[0] = %q, want common", scopeNames[0])
	}
	if scopeNames[1] != "ahost" {
		t.Errorf("scopeNames[1] = %q, want ahost", scopeNames[1])
	}
	if scopeNames[2] != "zhost" {
		t.Errorf("scopeNames[2] = %q, want zhost", scopeNames[2])
	}
}

func TestDoctor_AllAndHostCombined_Errors(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	_, err := svc.Doctor(context.Background(), "testhost", true, false, false)
	if err == nil {
		t.Fatal("expected error when --all and --host combined, got nil")
	}
}

func TestDoctor_HostScope_ScansCommonAndHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Create an invalid entry in common and a valid one in testhost.
	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	setupTrackedFile(t, repoPath, home, "testhost", ".vimrc", "\" vim")

	// Delete common storage to create an invalid entry.
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "testhost", false, false, false)
	if err != nil {
		t.Fatalf("Doctor --host: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with invalid common entry")
	}
	if report.Mode != "profile:testhost" {
		t.Errorf("Mode = %q, want profile:testhost", report.Mode)
	}
}

// ---------- Doctor fix tests ----------

func TestDoctor_Fix_MarkerMissing(t *testing.T) {
	svc, _ := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	// Confirm marker is absent.
	if testhelpers.FileExists(t, filepath.Join(repoPath, ".lnkrepo")) {
		t.Fatal("expected no marker file in v1 legacy repo")
	}

	commitsBefore := testhelpers.GitLog(t, repoPath)

	report, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor --fix: %v", err)
	}

	if !report.MarkerFixed {
		t.Error("expected MarkerFixed=true")
	}
	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".lnkrepo")) {
		t.Error("expected marker file created by fix")
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore)+1 {
		t.Errorf("expected 1 new commit after fix, before=%d after=%d",
			len(commitsBefore), len(commitsAfter))
	}
}

func TestDoctor_Fix_InvalidEntry_Removed(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	// Also set up a valid file so the tracker isn't left empty.
	setupTrackedFile(t, repoPath, home, "common", ".vimrc", "\" vimrc")

	// Delete storage to make .bashrc invalid.
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.CommitDeletion(t, repoPath, "common.lnk/.bashrc")

	_, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor --fix: %v", err)
	}

	// Invalid entry should have been removed from tracker.
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
	// Valid entry should remain.
	testhelpers.AssertTracked(t, repoPath, ".vimrc")
}

func TestDoctor_Fix_BrokenSymlink_Repaired(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Remove the symlink to create a broken state.
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor --fix: %v", err)
	}

	if !report.BrokenSymlinkFix {
		t.Error("expected BrokenSymlinkFix=true")
	}

	// Symlink should be restored.
	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestDoctor_Fix_AllScope_SkipsBrokenSymlinkRepair(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	_, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Remove symlink to create a broken state.
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", true, true, false)
	if err != nil {
		t.Fatalf("Doctor --all --fix: %v", err)
	}

	if !report.BrokenSymlinkFixSkipped {
		t.Error("expected BrokenSymlinkFixSkipped=true in --all mode")
	}

	// Symlink should not have been repaired.
	if testhelpers.FileExists(t, livePath) {
		t.Error("expected symlink to remain unrepaired in --all mode")
	}
}

func TestDoctor_Fix_NoIssues_NoCommit(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	commitsBefore := testhelpers.GitLog(t, repoPath)

	report, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor --fix: %v", err)
	}

	if report.HasIssues() {
		t.Error("expected no issues on clean repo")
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore) {
		t.Errorf("expected no new commit when no issues, before=%d after=%d",
			len(commitsBefore), len(commitsAfter))
	}
}

func TestDoctor_UninitializedRepo(t *testing.T) {
	svc, _ := testhelpers.NewTestRepo(t)

	_, err := svc.Doctor(context.Background(), "", false, false, false)
	if err == nil {
		t.Fatal("expected error on uninitialized repo, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNotInitialized) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotInitialized)
	}
}

func TestDoctor_Fix_DirtyTree_Errors(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Create an uncommitted change so the working tree is dirty.
	dirtyFile := filepath.Join(repoPath, "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Doctor(context.Background(), "", false, true, false)
	if err == nil {
		t.Fatal("expected error when fixing with dirty tree, got nil")
	}
	if !errors.Is(err, lnkerror.ErrDirtyTree) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrDirtyTree)
	}
}

func TestDoctor_Fix_BackupCollision(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Replace the managed symlink with a regular file and pre-create a backup.
	livePath := filepath.Join(home, ".bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, livePath, "# local version")
	testhelpers.MakeFile(t, livePath+".lnk-backup", "# existing backup")

	_, err := svc.Doctor(context.Background(), "", false, true, false)
	if err == nil {
		t.Fatal("expected error when doctor fix encounters existing backup, got nil")
	}
	if !errors.Is(err, lnkerror.ErrBackupExists) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrBackupExists)
	}
}

// ---------- HasIssues tests ----------

func TestDoctorReport_HasIssues_MarkerMissing(t *testing.T) {
	svc, _ := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()
	os.Remove(filepath.Join(repoPath, ".lnkrepo"))

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !report.HasIssues() {
		t.Error("expected HasIssues=true when marker missing")
	}
}

func TestDoctorReport_HasIssues_False_OnCleanRepo(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if report.HasIssues() {
		t.Error("expected HasIssues=false on clean repo")
	}
}

// ---------- Format variant tests ----------

func TestDoctor_V1_Scan_InvalidEntry(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor v1: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with invalid v1 entry")
	}
}

func TestDoctor_V1_Fix_InvalidEntry(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	setupTrackedFile(t, repoPath, home, "common", ".vimrc", "\" vimrc")
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.CommitDeletion(t, repoPath, ".bashrc")

	_, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor v1 --fix: %v", err)
	}

	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
	testhelpers.AssertTracked(t, repoPath, ".vimrc")
}

func TestDoctor_V1Legacy_Scan_InvalidEntry(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor v1 legacy: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with invalid v1 legacy entry")
	}
}

func TestDoctor_V1Legacy_Fix_InvalidEntry(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	storagePath, _ := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	setupTrackedFile(t, repoPath, home, "common", ".vimrc", "\" vimrc")
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}
	testhelpers.CommitDeletion(t, repoPath, ".bashrc")

	_, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor v1 legacy --fix: %v", err)
	}

	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
	testhelpers.AssertTracked(t, repoPath, ".vimrc")
}

// ---------- Doctor --prune-empty scan tests ----------

func TestDoctor_PruneEmpty_Scan_DetectsEmptyScope(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Set up a host scope with one file, then move it out to leave an empty tracker.
	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")

	// Write an empty tracker file directly to simulate a scope with no items.
	emptyTracker := filepath.Join(repoPath, ".lnk.emptyhost")
	if err := os.WriteFile(emptyTracker, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, true)
	if err != nil {
		t.Fatalf("Doctor --prune-empty scan: %v", err)
	}

	if len(report.EmptyScopes) != 1 || report.EmptyScopes[0] != "emptyhost" {
		t.Errorf("EmptyScopes = %v, want [emptyhost]", report.EmptyScopes)
	}
	if !report.HasIssues() {
		t.Error("expected HasIssues=true with empty scope detected")
	}
}

func TestDoctor_PruneEmpty_Scan_IgnoresNonEmptyScopes(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")

	report, err := svc.Doctor(context.Background(), "", false, false, true)
	if err != nil {
		t.Fatalf("Doctor --prune-empty scan: %v", err)
	}

	if len(report.EmptyScopes) != 0 {
		t.Errorf("EmptyScopes = %v, want [] for non-empty scope", report.EmptyScopes)
	}
}

func TestDoctor_PruneEmpty_Scan_IgnoresCommonScope(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	// Common scope is always present and may be empty — should never appear
	// in EmptyScopes since pruning common makes no sense.
	report, err := svc.Doctor(context.Background(), "", false, false, true)
	if err != nil {
		t.Fatalf("Doctor --prune-empty scan: %v", err)
	}

	for _, scope := range report.EmptyScopes {
		if scope == "common" {
			t.Error("common scope should never appear in EmptyScopes")
		}
	}
}

func TestDoctor_PruneEmpty_Scan_MultipleEmptyScopes(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Write two empty tracker files.
	for _, host := range []string{"ahost", "zhost"} {
		p := filepath.Join(repoPath, ".lnk."+host)
		if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := svc.Doctor(context.Background(), "", false, false, true)
	if err != nil {
		t.Fatalf("Doctor --prune-empty scan: %v", err)
	}

	// Should be sorted alphabetically.
	if len(report.EmptyScopes) != 2 {
		t.Fatalf("EmptyScopes = %v, want [ahost zhost]", report.EmptyScopes)
	}
	if report.EmptyScopes[0] != "ahost" {
		t.Errorf("EmptyScopes[0] = %q, want ahost", report.EmptyScopes[0])
	}
	if report.EmptyScopes[1] != "zhost" {
		t.Errorf("EmptyScopes[1] = %q, want zhost", report.EmptyScopes[1])
	}
}

// ---------- Doctor --prune-empty fix tests ----------

func TestDoctor_PruneEmpty_Fix_RemovesTrackerFile(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	testhelpers.CommitEmptyScope(t, repoPath, "emptyhost")
	emptyTracker := filepath.Join(repoPath, ".lnk.emptyhost")

	report, err := svc.Doctor(context.Background(), "", false, true, true)
	if err != nil {
		t.Fatalf("Doctor --fix --prune-empty: %v", err)
	}

	if len(report.PrunedScopes) != 1 || report.PrunedScopes[0] != "emptyhost" {
		t.Errorf("PrunedScopes = %v, want [emptyhost]", report.PrunedScopes)
	}
	if testhelpers.FileExists(t, emptyTracker) {
		t.Error("expected empty tracker file removed after prune")
	}
}

func TestDoctor_PruneEmpty_Fix_RemovesEmptyStorageDir(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Create empty tracker committed to git and empty storage directory.
	testhelpers.CommitEmptyScope(t, repoPath, "emptyhost")
	storageDir := filepath.Join(repoPath, "emptyhost.lnk")
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Doctor(context.Background(), "", false, true, true)
	if err != nil {
		t.Fatalf("Doctor --fix --prune-empty: %v", err)
	}

	if testhelpers.FileExists(t, storageDir) {
		t.Error("expected empty storage directory removed after prune")
	}
}

func TestDoctor_PruneEmpty_Fix_LeavesNonEmptyStorageDir(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Empty tracker committed to git, storage dir has a file.
	testhelpers.CommitEmptyScope(t, repoPath, "emptyhost")
	emptyTracker := filepath.Join(repoPath, ".lnk.emptyhost")
	storageDir := filepath.Join(repoPath, "emptyhost.lnk")
	testhelpers.MakeFile(t, filepath.Join(storageDir, "orphan"), "still here")
	testhelpers.CommitFile(t, repoPath, "emptyhost.lnk/orphan")

	_, err := svc.Doctor(context.Background(), "", false, true, true)
	if err != nil {
		t.Fatalf("Doctor --fix --prune-empty: %v", err)
	}

	// Tracker removed but storage dir should remain since it's not empty.
	if testhelpers.FileExists(t, emptyTracker) {
		t.Error("expected tracker file removed")
	}
	if !testhelpers.FileExists(t, storageDir) {
		t.Error("expected non-empty storage dir to remain")
	}
}

func TestDoctor_PruneEmpty_Fix_CommitCreated(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	testhelpers.CommitEmptyScope(t, repoPath, "emptyhost")

	commitsBefore := testhelpers.GitLog(t, repoPath)

	_, err := svc.Doctor(context.Background(), "", false, true, true)
	if err != nil {
		t.Fatalf("Doctor --fix --prune-empty: %v", err)
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore)+1 {
		t.Errorf("expected 1 new commit after prune, before=%d after=%d",
			len(commitsBefore), len(commitsAfter))
	}
}

func TestDoctor_PruneEmpty_Fix_SkippedForSpecificHost(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Create an empty scope.
	testhelpers.CommitEmptyScope(t, repoPath, "emptyhost")
	emptyTracker := filepath.Join(repoPath, ".lnk.emptyhost")

	commitsBefore := testhelpers.GitLog(t, repoPath)

	// --prune-empty with a specific host should not prune empty scopes.
	report, err := svc.Doctor(context.Background(), "testhost", false, true, true)
	if err != nil {
		t.Fatalf("Doctor --host --fix --prune-empty: %v", err)
	}

	if len(report.PrunedScopes) != 0 {
		t.Errorf("expected no pruned scopes when --host specified, got %v", report.PrunedScopes)
	}
	if !testhelpers.FileExists(t, emptyTracker) {
		t.Error("expected tracker file to remain when --host specified")
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore) {
		t.Errorf("expected no new commit when prune skipped, before=%d after=%d",
			len(commitsBefore), len(commitsAfter))
	}
}

func TestDoctor_PruneEmpty_Fix_AllScope(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	testhelpers.CommitEmptyScope(t, repoPath, "emptyhost")
	emptyTracker := filepath.Join(repoPath, ".lnk.emptyhost")

	report, err := svc.Doctor(context.Background(), "", true, true, true)
	if err != nil {
		t.Fatalf("Doctor --all --fix --prune-empty: %v", err)
	}

	if len(report.PrunedScopes) != 1 || report.PrunedScopes[0] != "emptyhost" {
		t.Errorf("PrunedScopes = %v, want [emptyhost]", report.PrunedScopes)
	}
	if testhelpers.FileExists(t, emptyTracker) {
		t.Error("expected empty tracker removed in --all mode")
	}
}

// ---------- Cross-scope tests ----------

func TestDoctor_FlagsCrossScopeCollision(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	testhelpers.InitGitRepo(t, repoDir)

	relativePath := "repos/hermes/.cursor/rules.md"

	// Manually track a project file in common scope to simulate a bad state.
	setupTrackedFile(t, repoPath, home, "common", relativePath, "# rules")

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !report.HasIssues() {
		t.Error("expected HasIssues=true with cross-scope collision")
	}

	found := false
	for _, result := range report.ScopeResults {
		if result.Name == "cross-scope" && result.Label == "inside git repo" {
			for _, item := range result.Items {
				if item == relativePath {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected cross-scope result containing %q, got %+v", relativePath, report.ScopeResults)
	}
}
