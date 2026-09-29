package service_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// ---------- Add tests ----------

func TestAdd_SingleFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Original path should now be a symlink.
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, filePath, storagePath)

	// File should have moved to repo storage.
	if !testhelpers.FileExists(t, storagePath) {
		t.Errorf("expected file at storage path %q", storagePath)
	}

	// Tracker should contain the relative path.
	testhelpers.AssertTracked(t, repoPath, ".bashrc")

	// Should have a commit.
	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) < 2 {
		t.Errorf("expected at least 2 commits (init + add), got %d", len(commits))
	}
}

func TestAdd_MultipleFiles(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".vimrc"),
	}
	for _, p := range paths {
		testhelpers.MakeFile(t, p, "# "+filepath.Base(p))
	}

	if err := svc.Add(context.Background(), "", paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, p := range paths {
		rel := filepath.Base(p)
		storagePath := filepath.Join(repoPath, "common.lnk", rel)
		testhelpers.AssertSymlink(t, p, storagePath)
		testhelpers.AssertTracked(t, repoPath, rel)
	}

	// All files should land in a single commit.
	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 2 {
		t.Errorf("expected 2 commits (init + add), got %d", len(commits))
	}
}

func TestAdd_Directory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	dirPath := filepath.Join(home, ".config", "nvim")
	testhelpers.MakeDir(t, dirPath)
	testhelpers.MakeFile(t, filepath.Join(dirPath, "init.lua"), "-- neovim config")

	if err := svc.Add(context.Background(), "", []string{dirPath}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "nvim")
	testhelpers.AssertSymlink(t, dirPath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/nvim")
}

func TestAdd_NestedFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".config", "git", "config")
	testhelpers.MakeFile(t, filePath, "[user]")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "git", "config")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/git/config")
}

// ---------- Failure cases ----------

func TestAdd_DuplicateInSameCall(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	err := svc.Add(context.Background(), "", []string{filePath, filePath})
	if err == nil {
		t.Fatal("expected error for duplicate path in same call, got nil")
	}
	if !errors.Is(err, lnkerror.ErrDuplicatePath) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrDuplicatePath)
	}
}

func TestAdd_AlreadyManaged(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	err := svc.Add(context.Background(), "", []string{filePath})
	if err == nil {
		t.Fatal("expected error adding already-managed path, got nil")
	}
	if !errors.Is(err, lnkerror.ErrAlreadyManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrAlreadyManaged)
	}
}

func TestAdd_PathOutsideHome(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	// /tmp is outside $HOME regardless of platform.
	outsidePath := filepath.Join(os.TempDir(), "lnk-test-outside")
	testhelpers.MakeFile(t, outsidePath, "outside")
	defer os.Remove(outsidePath)

	err := svc.Add(context.Background(), "", []string{outsidePath})
	if err == nil {
		t.Fatal("expected error for path outside $HOME, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNotInHome) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotInHome)
	}
}

func TestAdd_NonExistentPath(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	err := svc.Add(context.Background(), "", []string{filepath.Join(home, "does-not-exist")})
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

func TestAdd_RollbackOnPartialFailure(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// First file exists, second does not — Add should fail and roll back the first.
	existing := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, existing, "# bashrc")
	missing := filepath.Join(home, "does-not-exist")

	_ = svc.Add(context.Background(), "", []string{existing, missing})

	// Original file should be restored, not left in repo storage.
	if !testhelpers.FileExists(t, existing) {
		t.Error("expected original file to be restored after rollback")
	}
	info, err := os.Lstat(existing)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected original path to be a regular file after rollback, not a symlink")
	}

	// Tracker should be empty.
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
}

func TestAdd_EmptyPaths(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	err := svc.Add(context.Background(), "", []string{})
	if err == nil {
		t.Fatal("expected error for empty paths, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNoPaths) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNoPaths)
	}
}

func TestAdd_PathIsSymlink(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	// Create a symlink where the user wants to add — lnk cannot manage symlinks.
	filePath := filepath.Join(home, ".bashrc")
	targetPath := filepath.Join(home, ".real-bashrc")
	testhelpers.MakeFile(t, targetPath, "# real bashrc")
	if err := os.Symlink(targetPath, filePath); err != nil {
		t.Fatal(err)
	}

	err := svc.Add(context.Background(), "", []string{filePath})
	if err == nil {
		t.Fatal("expected error when adding a symlink, got nil")
	}
	if !errors.Is(err, fs.ErrUnsupportedType) {
		t.Errorf("error = %v, want %v", err, fs.ErrUnsupportedType)
	}
}

// ---------- Host-scope Add tests ----------

func TestAdd_HostScope_ExplicitHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	if err := svc.Add(context.Background(), "testhost", []string{filePath}); err != nil {
		t.Fatalf("Add with host: %v", err)
	}

	// Should be stored under testhost.lnk/, not common.lnk/.
	hostStorage := filepath.Join(repoPath, "testhost.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, filePath, hostStorage)

	if !testhelpers.FileExists(t, hostStorage) {
		t.Errorf("expected file at host storage path %q", hostStorage)
	}

	// Should appear in the host tracker, not the common tracker.
	testhelpers.AssertTrackedInScope(t, repoPath, "testhost", ".bashrc")
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
}

func TestAdd_HostScope_CommonIsNormalized(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	// Passing "common" explicitly should behave identically to passing "".
	if err := svc.Add(context.Background(), "common", []string{filePath}); err != nil {
		t.Fatalf("Add with common: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")
}

func TestAdd_HostScope_SamePathInDifferentScopes(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	// Add to common first.
	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add to common: %v", err)
	}

	// Adding the same path to a host scope should fail — it's already owned.
	err := svc.Add(context.Background(), "testhost", []string{filePath})
	if err == nil {
		t.Fatal("expected error adding already-managed path to a different scope, got nil")
	}
	if !errors.Is(err, lnkerror.ErrAlreadyManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrAlreadyManaged)
	}
}

func TestAdd_HostScope_MultipleFiles(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".vimrc"),
	}
	for _, p := range paths {
		testhelpers.MakeFile(t, p, "# "+filepath.Base(p))
	}

	if err := svc.Add(context.Background(), "testhost", paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, p := range paths {
		rel := filepath.Base(p)
		storagePath := filepath.Join(repoPath, "testhost.lnk", rel)
		testhelpers.AssertSymlink(t, p, storagePath)
		testhelpers.AssertTrackedInScope(t, repoPath, "testhost", rel)
		testhelpers.AssertNotTracked(t, repoPath, rel)
	}

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 2 {
		t.Errorf("expected 2 commits (init + add), got %d", len(commits))
	}
}

// ---------- V1 format Add tests ----------

func TestAdd_V1_SingleFile(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}

	// v1 stores files in the repo root, not common.lnk/.
	storagePath := filepath.Join(repoPath, ".bashrc")
	testhelpers.AssertSymlink(t, filePath, storagePath)

	if !testhelpers.FileExists(t, storagePath) {
		t.Errorf("expected file at v1 storage path %q", storagePath)
	}

	// v1 tracker is .lnk, not .lnk.common.
	testhelpers.AssertTracked(t, repoPath, ".bashrc")

	if testhelpers.FileExists(t, filepath.Join(repoPath, "common.lnk")) {
		t.Error("v1 repo should not have a common.lnk directory")
	}
}

func TestAdd_V1_NestedFile(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".config", "git", "config")
	testhelpers.MakeFile(t, filePath, "[user]")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}

	storagePath := filepath.Join(repoPath, ".config", "git", "config")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/git/config")
}

func TestAdd_V1_MultipleFiles(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".vimrc"),
	}
	for _, p := range paths {
		testhelpers.MakeFile(t, p, "# "+filepath.Base(p))
	}

	if err := svc.Add(context.Background(), "", paths); err != nil {
		t.Fatalf("Add v1: %v", err)
	}

	for _, p := range paths {
		rel := filepath.Base(p)
		storagePath := filepath.Join(repoPath, rel)
		testhelpers.AssertSymlink(t, p, storagePath)
		testhelpers.AssertTracked(t, repoPath, rel)
	}

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 2 {
		t.Errorf("expected 2 commits (init + add), got %d", len(commits))
	}
}

// ---------- V1 legacy (no marker) Add tests ----------

func TestAdd_V1Legacy_SingleFile(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add v1 legacy: %v", err)
	}

	// v1 stores files in repo root.
	storagePath := filepath.Join(repoPath, ".bashrc")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".bashrc")

	// Must not have created a common.lnk directory or v2 tracker.
	if testhelpers.FileExists(t, filepath.Join(repoPath, "common.lnk")) {
		t.Error("v1 legacy repo should not have a common.lnk directory")
	}
	if testhelpers.FileExists(t, filepath.Join(repoPath, ".lnk.common")) {
		t.Error("v1 legacy repo should not have a .lnk.common tracker file")
	}
}

func TestAdd_V1Legacy_NestedFile(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".config", "git", "config")
	testhelpers.MakeFile(t, filePath, "[user]")

	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add v1 legacy: %v", err)
	}

	storagePath := filepath.Join(repoPath, ".config", "git", "config")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/git/config")
}

func TestAdd_V1Legacy_MultipleFiles(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".vimrc"),
	}
	for _, p := range paths {
		testhelpers.MakeFile(t, p, "# "+filepath.Base(p))
	}

	if err := svc.Add(context.Background(), "", paths); err != nil {
		t.Fatalf("Add v1 legacy: %v", err)
	}

	for _, p := range paths {
		rel := filepath.Base(p)
		testhelpers.AssertSymlink(t, p, filepath.Join(repoPath, rel))
		testhelpers.AssertTracked(t, repoPath, rel)
	}

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 2 {
		t.Errorf("expected 2 commits (init + add), got %d", len(commits))
	}
}

func TestAdd_V1Legacy_PreservesPriorEntries(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	// Simulate a file that was tracked before the service version under test —
	// write an entry directly into the .lnk file and commit it, then add a new file.
	existingEntry := ".vimrc"
	lnkPath := filepath.Join(repoPath, ".lnk")
	if err := os.WriteFile(lnkPath, []byte(existingEntry+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create the file in storage to satisfy tracker validation.
	testhelpers.MakeFile(t, filepath.Join(repoPath, existingEntry), "\" vimrc")

	commitCmds := [][]string{
		{"git", "-C", repoPath, "add", "."},
		{"git", "-C", repoPath, "commit", "-m", "lnk: added .vimrc"},
	}
	for _, c := range commitCmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	// Now add a new file via the service.
	newFile := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, newFile, "# bashrc")

	if err := svc.Add(context.Background(), "", []string{newFile}); err != nil {
		t.Fatalf("Add v1 legacy: %v", err)
	}

	// Both entries should be present in the tracker.
	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertTracked(t, repoPath, ".vimrc")
}

// ---------- Git boundary tests ----------

func TestAdd_RefusesProjectFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	testhelpers.InitGitRepo(t, repoDir)

	projectFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, projectFile, "# rules")

	err := svc.Add(context.Background(), "", []string{projectFile})
	if err == nil {
		t.Fatal("expected error adding project file, got nil")
	}
	if !errors.Is(err, lnkerror.ErrInsideGitRepo) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrInsideGitRepo)
	}
}

func TestAdd_NonExistentPathInsideGitRepo(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	testhelpers.InitGitRepo(t, repoDir)

	// A ! negation (or any pattern) is project-scope syntax; host scope should
	// point at the right command instead of a bare "file not found".
	err := svc.Add(context.Background(), "", []string{filepath.Join(repoDir, "!AGENTS.md")})
	if err == nil {
		t.Fatal("expected error for pattern-like path inside a git repo")
	}
	if !errors.Is(err, lnkerror.ErrInsideGitRepo) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrInsideGitRepo)
	}
}

func TestAdd_AcceptsDotfileFromInsideRepo(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	testhelpers.InitGitRepo(t, repoDir)

	dotfile := filepath.Join(home, ".vimrc")
	testhelpers.MakeFile(t, dotfile, "# vimrc")

	if err := svc.Add(context.Background(), "", []string{dotfile}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".vimrc")
	testhelpers.AssertSymlink(t, dotfile, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".vimrc")
}
