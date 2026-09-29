package service_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// setupV2Repo creates a v2 repo with a set of tracked files ready for format
// migration testing. Returns the service and home path.
func setupV2Repo(t *testing.T) (svc *service.Service, home string) {
	t.Helper()
	svc, home = testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	setupTrackedFile(t, repoPath, home, "common", ".vimrc", "\" vimrc")
	setupTrackedFile(t, repoPath, home, "common", ".config/git/config", "[user]")

	return svc, home
}

// setupV1Repo creates a v1 repo with a set of tracked files ready for format
// migration testing. Returns the service and home path.
func setupV1Repo(t *testing.T) (svc *service.Service, home string) {
	t.Helper()
	svc, home = testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	setupTrackedFile(t, repoPath, home, "common", ".vimrc", "\" vimrc")
	setupTrackedFile(t, repoPath, home, "common", ".config/git/config", "[user]")

	return svc, home
}

// assertSymlinkBroken verifies that path is a symlink whose target no longer exists.
// After a format migration, symlinks are intentionally stale — Doctor --fix repairs them.
func assertSymlinkBroken(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%q is not a symlink", path)
		return
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("Readlink(%q): %v", path, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("expected symlink %q to be broken after format migration, but target %q still exists", path, target)
	}
}

// ---------- Format tests ----------

func TestFormat_ReportVersion_V2(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	result, err := svc.Format(context.Background(), false, false)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(result, "v2") {
		t.Errorf("result = %q, want mention of v2", result)
	}
}

func TestFormat_ReportVersion_V1(t *testing.T) {
	svc, _ := testhelpers.TestHomeV1(t)

	result, err := svc.Format(context.Background(), false, false)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(result, "v1") {
		t.Errorf("result = %q, want mention of v1", result)
	}
}

func TestFormat_AlreadyV2(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	result, err := svc.Format(context.Background(), false, true)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(result, "already") {
		t.Errorf("result = %q, want mention of already", result)
	}
}

func TestFormat_AlreadyV1(t *testing.T) {
	svc, _ := testhelpers.TestHomeV1(t)

	result, err := svc.Format(context.Background(), true, false)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(result, "already") {
		t.Errorf("result = %q, want mention of already", result)
	}
}

func TestFormat_V2ToV1_StoragePaths(t *testing.T) {
	svc, _ := setupV2Repo(t)
	repoPath := svc.RepoPath()

	if _, err := svc.Format(context.Background(), true, false); err != nil {
		t.Fatalf("Format v2→v1: %v", err)
	}

	// All files should now be in the repo root, not common.lnk/.
	for _, rel := range []string{".bashrc", ".vimrc", ".config/git/config"} {
		v1Path := filepath.Join(repoPath, rel)
		v2Path := filepath.Join(repoPath, "common.lnk", rel)

		if !testhelpers.FileExists(t, v1Path) {
			t.Errorf("expected file at v1 storage path %q", v1Path)
		}
		if testhelpers.FileExists(t, v2Path) {
			t.Errorf("expected file removed from v2 storage path %q", v2Path)
		}
	}
}

func TestFormat_V2ToV1_TrackerFile(t *testing.T) {
	svc, _ := setupV2Repo(t)
	repoPath := svc.RepoPath()

	if _, err := svc.Format(context.Background(), true, false); err != nil {
		t.Fatalf("Format v2→v1: %v", err)
	}

	// v1 tracker should be .lnk, not .lnk.common.
	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk")) {
		t.Error("expected v1 tracker file .lnk to exist")
	}
	if testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk.common")) {
		t.Error("expected v2 tracker file .lnk.common to be removed")
	}

	// All entries should be present in the v1 tracker.
	for _, rel := range []string{".bashrc", ".vimrc", ".config/git/config"} {
		testhelpers.AssertTracked(t, repoPath, rel)
	}
}

func TestFormat_V2ToV1_MarkerFile(t *testing.T) {
	svc, _ := setupV2Repo(t)
	repoPath := svc.RepoPath()

	if _, err := svc.Format(context.Background(), true, false); err != nil {
		t.Fatalf("Format v2→v1: %v", err)
	}

	marker, err := os.ReadFile(filepath.Join(repoPath, ".lnkrepo"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(marker), "version=1") {
		t.Errorf("marker = %q, want version=1", string(marker))
	}
}

func TestFormat_V2ToV1_SymlinksStaleAfterMigration(t *testing.T) {
	svc, home := setupV2Repo(t)

	if _, err := svc.Format(context.Background(), true, false); err != nil {
		t.Fatalf("Format v2→v1: %v", err)
	}

	// Symlinks at live paths are intentionally stale after migration.
	// They still point to the old common.lnk/ paths which no longer exist.
	// Doctor --fix is responsible for repairing them.
	for _, rel := range []string{".bashrc", ".vimrc", ".config/git/config"} {
		assertSymlinkBroken(t, filepath.Join(home, rel))
	}
}

func TestFormat_V2ToV1_CommitCreated(t *testing.T) {
	svc, _ := setupV2Repo(t)
	repoPath := svc.RepoPath()

	commitsBefore := testhelpers.GitLog(t, repoPath)

	if _, err := svc.Format(context.Background(), true, false); err != nil {
		t.Fatalf("Format v2→v1: %v", err)
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore)+1 {
		t.Errorf("expected 1 new commit after format, before=%d after=%d",
			len(commitsBefore), len(commitsAfter))
	}
}

func TestFormat_V1ToV2_StoragePaths(t *testing.T) {
	svc, _ := setupV1Repo(t)
	repoPath := svc.RepoPath()

	if _, err := svc.Format(context.Background(), false, true); err != nil {
		t.Fatalf("Format v1→v2: %v", err)
	}

	for _, rel := range []string{".bashrc", ".vimrc", ".config/git/config"} {
		v2Path := filepath.Join(repoPath, "common.lnk", rel)
		v1Path := filepath.Join(repoPath, rel)

		if !testhelpers.FileExists(t, v2Path) {
			t.Errorf("expected file at v2 storage path %q", v2Path)
		}
		if testhelpers.FileExists(t, v1Path) {
			t.Errorf("expected file removed from v1 storage path %q", v1Path)
		}
	}
}

func TestFormat_V1ToV2_TrackerFile(t *testing.T) {
	svc, _ := setupV1Repo(t)
	repoPath := svc.RepoPath()

	if _, err := svc.Format(context.Background(), false, true); err != nil {
		t.Fatalf("Format v1→v2: %v", err)
	}

	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk.common")) {
		t.Error("expected v2 tracker file .lnk.common to exist")
	}
	if testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk")) {
		t.Error("expected v1 tracker file .lnk to be removed")
	}

	for _, rel := range []string{".bashrc", ".vimrc", ".config/git/config"} {
		testhelpers.AssertTracked(t, repoPath, rel)
	}
}

func TestFormat_V1ToV2_MarkerFile(t *testing.T) {
	svc, _ := setupV1Repo(t)
	repoPath := svc.RepoPath()

	if _, err := svc.Format(context.Background(), false, true); err != nil {
		t.Fatalf("Format v1→v2: %v", err)
	}

	marker, err := os.ReadFile(filepath.Join(repoPath, ".lnkrepo"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(marker), "version=2") {
		t.Errorf("marker = %q, want version=2", string(marker))
	}
}

func TestFormat_V1ToV2_SymlinksStaleAfterMigration(t *testing.T) {
	svc, home := setupV1Repo(t)

	if _, err := svc.Format(context.Background(), false, true); err != nil {
		t.Fatalf("Format v1→v2: %v", err)
	}

	// Symlinks are intentionally stale after migration — Doctor --fix repairs them.
	for _, rel := range []string{".bashrc", ".vimrc", ".config/git/config"} {
		assertSymlinkBroken(t, filepath.Join(home, rel))
	}
}

func TestFormat_V1ToV2_CommitCreated(t *testing.T) {
	svc, _ := setupV1Repo(t)
	repoPath := svc.RepoPath()

	commitsBefore := testhelpers.GitLog(t, repoPath)

	if _, err := svc.Format(context.Background(), false, true); err != nil {
		t.Fatalf("Format v1→v2: %v", err)
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore)+1 {
		t.Errorf("expected 1 new commit after format, before=%d after=%d",
			len(commitsBefore), len(commitsAfter))
	}
}

func TestFormat_V1Legacy_MigratestoV2AndCreatesMarker(t *testing.T) {
	svc, _ := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	_, err := svc.Format(context.Background(), false, true)
	if err != nil {
		t.Fatalf("Format v1 legacy→v2: %v", err)
	}

	// Migration should have created the marker file.
	marker, err := os.ReadFile(filepath.Join(repoPath, ".lnkrepo"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(marker), "version=2") {
		t.Errorf("marker = %q, want version=2", string(marker))
	}

	// v2 tracker should exist.
	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk.common")) {
		t.Error("expected .lnk.common after v1 legacy→v2 migration")
	}
	if testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk")) {
		t.Error("expected .lnk removed after migration")
	}
}

// ---------- Failure cases ----------

func TestFormat_BothFlagsSet(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	_, err := svc.Format(context.Background(), true, true)
	if err == nil {
		t.Fatal("expected error when both --v1 and --v2 are set, got nil")
	}
}

func TestFormat_UninitializedRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	repoPath := filepath.Join(home, ".config", "lnk")
	svc := service.New(repoPath)

	_, err := svc.Format(context.Background(), false, true)
	if err == nil {
		t.Fatal("expected error for uninitialized repo, got nil")
	}
}
