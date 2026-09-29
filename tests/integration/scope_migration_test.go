//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// TestIntegration_ScopeMigration simulates a user moving tracked files between
// common and host scopes, verifying that List reflects the correct state after
// each Move and that symlinks are repointed to the new storage location.
func TestIntegration_ScopeMigration(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// --- Step 1: Add files to common scope ---
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
		t.Fatalf("Add common: %v", err)
	}

	// Verify all in common scope.
	result, err := svc.List(context.Background(), "", true)
	if err != nil {
		t.Fatalf("List after Add: %v", err)
	}
	if len(result.Scopes) != 1 || result.Scopes[0].Name != "common" {
		t.Fatalf("expected 1 common scope, got %v", scopeNames(result))
	}
	if len(result.Scopes[0].Items) != len(files) {
		t.Errorf("expected %d common items, got %d", len(files), len(result.Scopes[0].Items))
	}

	// --- Step 2: Move .bashrc from common to testhost ---
	bashrcLive := filepath.Join(home, ".bashrc")
	if err := svc.Move(context.Background(), bashrcLive, "testhost", false); err != nil {
		t.Fatalf("Move common→testhost: %v", err)
	}

	// Verify List shows common with 2 items and testhost with 1.
	result, err = svc.List(context.Background(), "", true)
	if err != nil {
		t.Fatalf("List after Move common→testhost: %v", err)
	}
	if len(result.Scopes) != 2 {
		t.Fatalf("expected 2 scopes after move, got %d: %v", len(result.Scopes), scopeNames(result))
	}
	if result.Scopes[0].Name != "common" || len(result.Scopes[0].Items) != 2 {
		t.Errorf("common scope = %v items, want 2", result.Scopes[0].Items)
	}
	if result.Scopes[1].Name != "testhost" || len(result.Scopes[1].Items) != 1 {
		t.Errorf("testhost scope = %v items, want 1", result.Scopes[1].Items)
	}

	// Symlink should now point to testhost storage.
	testhelpers.AssertSymlink(t, bashrcLive, filepath.Join(repoPath, "testhost.lnk", ".bashrc"))
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
	testhelpers.AssertTrackedInScope(t, repoPath, "testhost", ".bashrc")

	// --- Step 3: Move .bashrc back to common using bool flag ---
	if err := svc.Move(context.Background(), bashrcLive, "", true); err != nil {
		t.Fatalf("Move testhost→common (bool): %v", err)
	}

	result, err = svc.List(context.Background(), "", true)
	if err != nil {
		t.Fatalf("List after Move testhost→common: %v", err)
	}
	if len(result.Scopes[0].Items) != len(files) {
		t.Errorf("expected %d common items after move back, got %d", len(files), len(result.Scopes[0].Items))
	}

	// Symlink should point back to common storage.
	testhelpers.AssertSymlink(t, bashrcLive, filepath.Join(repoPath, "common.lnk", ".bashrc"))
	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")

	// --- Step 4: Move .bashrc to common using explicit "common" string ---
	// First move it back to testhost.
	if err := svc.Move(context.Background(), bashrcLive, "testhost", false); err != nil {
		t.Fatalf("Move common→testhost (setup): %v", err)
	}

	// Now move back using toHost="common" instead of toCommon=true.
	if err := svc.Move(context.Background(), bashrcLive, "common", false); err != nil {
		t.Fatalf("Move testhost→common (explicit string): %v", err)
	}

	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")
	testhelpers.AssertSymlink(t, bashrcLive, filepath.Join(repoPath, "common.lnk", ".bashrc"))

	// --- Step 5: Move multiple files to host scope and verify ordering in List ---
	vimrcLive := filepath.Join(home, ".vimrc")
	if err := svc.Move(context.Background(), vimrcLive, "testhost", false); err != nil {
		t.Fatalf("Move .vimrc common→testhost: %v", err)
	}

	result, err = svc.List(context.Background(), "", true)
	if err != nil {
		t.Fatalf("List after moving .vimrc: %v", err)
	}

	// Scopes: common first, then testhost alphabetically.
	if len(result.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d: %v", len(result.Scopes), scopeNames(result))
	}
	if result.Scopes[0].Name != "common" {
		t.Errorf("scopes[0] = %q, want common", result.Scopes[0].Name)
	}
	if result.Scopes[1].Name != "testhost" {
		t.Errorf("scopes[1] = %q, want testhost", result.Scopes[1].Name)
	}

	// --- Step 6: Verify commit count reflects all operations ---
	// init + add(3) + move×4 + move×1 = at least 7 commits.
	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) < 7 {
		t.Errorf("expected at least 7 commits, got %d", len(commits))
	}
}

// scopeNames extracts scope names from a ListResult for failure messages.
func scopeNames(result service.ListResult) []string {
	names := make([]string, len(result.Scopes))
	for i, s := range result.Scopes {
		names[i] = s.Name
	}
	return names
}
