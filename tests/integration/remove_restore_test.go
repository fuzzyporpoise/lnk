//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// TestIntegration_RemoveRestoreCycle simulates a user removing a tracked file
// and then re-adding it, verifying the file is correctly restored to the live
// path after Remove and correctly re-tracked after a second Add.
func TestIntegration_RemoveRestoreCycle(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// --- Step 1: Create and track files ---
	files := map[string]string{
		".bashrc":            "# bashrc",
		".vimrc":             "\" vimrc",
		".config/git/config": "[user]",
	}
	paths := make([]string, 0, len(files))
	for rel, content := range files {
		p := filepath.Join(home, rel)
		testhelpers.MakeFile(t, p, content)
		paths = append(paths, p)
	}

	if err := svc.Add(context.Background(), "", paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify all symlinks are in place.
	for rel := range files {
		livePath := filepath.Join(home, rel)
		storagePath := filepath.Join(repoPath, "common.lnk", rel)
		testhelpers.AssertSymlink(t, livePath, storagePath)
	}

	// --- Step 2: Remove one file and verify it is restored to live path ---
	bashrcLive := filepath.Join(home, ".bashrc")
	if err := svc.Remove(context.Background(), "", bashrcLive); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Live path should now be a regular file, not a symlink.
	info, err := os.Lstat(bashrcLive)
	if err != nil {
		t.Fatalf("Lstat after Remove: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected regular file after Remove, got symlink")
	}

	// File content should be intact.
	content, err := os.ReadFile(bashrcLive)
	if err != nil {
		t.Fatalf("ReadFile after Remove: %v", err)
	}
	if string(content) != "# bashrc" {
		t.Errorf("content after Remove = %q, want %q", string(content), "# bashrc")
	}

	// Storage path should no longer exist.
	if testhelpers.FileExists(t, filepath.Join(repoPath, "common.lnk", ".bashrc")) {
		t.Error("expected storage file removed after Remove")
	}

	// Tracker should no longer contain .bashrc.
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")

	// Other files should still be tracked and symlinked.
	for rel := range files {
		if rel == ".bashrc" {
			continue
		}
		testhelpers.AssertTracked(t, repoPath, rel)
		testhelpers.AssertSymlink(t, filepath.Join(home, rel), filepath.Join(repoPath, "common.lnk", rel))
	}

	// --- Step 3: Re-add the file and verify it is re-tracked ---
	if err := svc.Add(context.Background(), "", []string{bashrcLive}); err != nil {
		t.Fatalf("Re-Add: %v", err)
	}

	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertSymlink(t, bashrcLive, filepath.Join(repoPath, "common.lnk", ".bashrc"))

	// Content should still be intact after re-add.
	content, err = os.ReadFile(bashrcLive)
	if err != nil {
		t.Fatalf("ReadFile after Re-Add: %v", err)
	}
	if string(content) != "# bashrc" {
		t.Errorf("content after Re-Add = %q, want %q", string(content), "# bashrc")
	}

	// --- Step 4: Verify List reflects final state ---
	result, err := svc.List(context.Background(), "", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(result.Scopes) != 1 {
		t.Fatalf("expected 1 scope, got %d", len(result.Scopes))
	}
	if len(result.Scopes[0].Items) != len(files) {
		t.Errorf("expected %d items after re-add, got %d: %v",
			len(files), len(result.Scopes[0].Items), result.Scopes[0].Items)
	}

	// --- Step 5: Forget a file and verify repo copy remains ---
	vimrcLive := filepath.Join(home, ".vimrc")
	vimrcStorage := filepath.Join(repoPath, "common.lnk", ".vimrc")

	if err := svc.Forget(context.Background(), "", vimrcLive); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	// Live symlink should be gone.
	if testhelpers.FileExists(t, vimrcLive) {
		t.Error("expected live symlink removed after Forget")
	}

	// Storage file should remain.
	if !testhelpers.FileExists(t, vimrcStorage) {
		t.Error("expected storage file to remain after Forget")
	}

	// Tracker should not contain .vimrc.
	testhelpers.AssertNotTracked(t, repoPath, ".vimrc")

	// --- Step 6: Final git log sanity check ---
	// Expect: init + add(3) + remove + add(1) + forget = 5 commits minimum.
	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) < 5 {
		t.Errorf("expected at least 5 commits, got %d: %v", len(commits), commits)
	}
}

// TestIntegration_RemoveHostScoped verifies Remove works correctly when a file
// is in a host scope, requiring --host to be specified.
func TestIntegration_RemoveHostScoped(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	if err := svc.Add(context.Background(), "testhost", []string{filePath}); err != nil {
		t.Fatalf("Add host: %v", err)
	}

	// Remove without --host should fail.
	if err := svc.Remove(context.Background(), "", filePath); err == nil {
		t.Fatal("expected error removing host-scoped file without --host, got nil")
	}

	// Remove with correct --host should succeed.
	if err := svc.Remove(context.Background(), "testhost", filePath); err != nil {
		t.Fatalf("Remove with host: %v", err)
	}

	info, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected regular file after Remove, got symlink")
	}

	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")

}
