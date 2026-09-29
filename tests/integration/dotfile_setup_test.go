//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// TestIntegration_FullDotfileSetup simulates a user setting up lnk for the
// first time: initialising a repo, tracking several files across common and
// host scopes, listing them to verify, then restoring on a fresh machine by
// removing all symlinks and calling Restore.
func TestIntegration_FullDotfileSetup(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// --- Step 1: Create files to track ---
	files := map[string]string{
		".bashrc":               "# bashrc",
		".vimrc":                "\" vimrc",
		".config/git/config":    "[user]\n\tname = Test",
		".config/nvim/init.lua": "-- neovim",
	}
	for rel, content := range files {
		testhelpers.MakeFile(t, filepath.Join(home, rel), content)
	}
	hostFile := filepath.Join(home, ".ssh/config")
	testhelpers.MakeFile(t, hostFile, "# ssh config")

	// --- Step 2: Add files to common scope ---
	commonPaths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".vimrc"),
		filepath.Join(home, ".config/git/config"),
		filepath.Join(home, ".config/nvim/init.lua"),
	}
	if err := svc.Add(context.Background(), "", commonPaths); err != nil {
		t.Fatalf("Add common: %v", err)
	}

	// --- Step 3: Add host-scoped file ---
	if err := svc.Add(context.Background(), "testhost", []string{hostFile}); err != nil {
		t.Fatalf("Add host: %v", err)
	}

	// --- Step 4: Verify via List ---
	result, err := svc.List(context.Background(), "", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(result.Scopes) != 2 {
		t.Fatalf("expected 2 scopes (common + testhost), got %d", len(result.Scopes))
	}
	if result.Scopes[0].Name != "common" {
		t.Errorf("scopes[0] = %q, want common", result.Scopes[0].Name)
	}
	if len(result.Scopes[0].Items) != 4 {
		t.Errorf("common items = %v, want 4", result.Scopes[0].Items)
	}
	if result.Scopes[1].Name != "testhost" {
		t.Errorf("scopes[1] = %q, want testhost", result.Scopes[1].Name)
	}
	if len(result.Scopes[1].Items) != 1 {
		t.Errorf("testhost items = %v, want 1", result.Scopes[1].Items)
	}

	// --- Step 5: Verify symlinks are in place ---
	for rel := range files {
		livePath := filepath.Join(home, rel)
		storagePath := filepath.Join(repoPath, "common.lnk", rel)
		testhelpers.AssertSymlink(t, livePath, storagePath)
	}
	testhelpers.AssertSymlink(t, hostFile, filepath.Join(repoPath, "testhost.lnk", ".ssh/config"))

	// --- Step 6: Simulate fresh machine — remove all symlinks ---
	for rel := range files {
		if err := os.Remove(filepath.Join(home, rel)); err != nil {
			t.Fatalf("remove symlink %s: %v", rel, err)
		}
	}
	if err := os.Remove(hostFile); err != nil {
		t.Fatalf("remove host symlink: %v", err)
	}

	// --- Step 7: Restore and verify symlinks are recreated ---
	info, err := svc.Restore(context.Background(), "testhost", false)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(info.Restored) != 5 {
		t.Errorf("Restored = %v, want 5 paths", info.Restored)
	}

	for rel := range files {
		livePath := filepath.Join(home, rel)
		storagePath := filepath.Join(repoPath, "common.lnk", rel)
		testhelpers.AssertSymlink(t, livePath, storagePath)
	}
	testhelpers.AssertSymlink(t, hostFile, filepath.Join(repoPath, "testhost.lnk", ".ssh/config"))

	// --- Step 8: Verify file contents are intact ---
	for rel, want := range files {
		content, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", rel, err)
		}
		if string(content) != want {
			t.Errorf("content of %s = %q, want %q", rel, string(content), want)
		}
	}
}
