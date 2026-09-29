//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// TestIntegration_FormatUpgrade simulates the full round-trip format migration:
// v2→v1→v2, verifying at each stage that symlinks are stale after Format and
// repaired after Doctor --fix, and that file contents are intact throughout.
func TestIntegration_FormatUpgrade(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// --- Step 1: Track files in v2 format ---
	files := map[string]string{
		".bashrc":            "# bashrc",
		".vimrc":             "\" vimrc",
		".config/git/config": "[user]\n\tname = Test",
		".tmux.conf":         "# tmux",
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

	// Verify initial v2 symlinks.
	for rel := range files {
		testhelpers.AssertSymlink(t,
			filepath.Join(home, rel),
			filepath.Join(repoPath, "common.lnk", rel),
		)
	}

	// --- Step 2: Format v2→v1 ---
	result, err := svc.Format(context.Background(), true, false)
	if err != nil {
		t.Fatalf("Format v2→v1: %v", err)
	}
	if !strings.Contains(result, "formatted") {
		t.Errorf("Format result = %q, want mention of formatted", result)
	}

	// Verify v1 storage layout — files should be in repo root.
	for rel := range files {
		v1Path := filepath.Join(repoPath, rel)
		v2Path := filepath.Join(repoPath, "common.lnk", rel)
		if !testhelpers.FileExists(t, v1Path) {
			t.Errorf("expected file at v1 storage path %q", v1Path)
		}
		if testhelpers.FileExists(t, v2Path) {
			t.Errorf("expected file removed from v2 storage path %q", v2Path)
		}
	}

	// Verify marker updated to v1.
	marker, err := os.ReadFile(filepath.Join(repoPath, ".lnkrepo"))
	if err != nil {
		t.Fatalf("read marker after v2→v1: %v", err)
	}
	if !strings.Contains(string(marker), "version=1") {
		t.Errorf("marker = %q, want version=1", string(marker))
	}

	// Symlinks should be stale — Format intentionally doesn't repair them.
	for rel := range files {
		livePath := filepath.Join(home, rel)
		info, err := os.Lstat(livePath)
		if err != nil {
			t.Fatalf("Lstat %s after v2→v1: %v", rel, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should still be a symlink (stale) after Format", rel)
		}
		// Target should no longer exist.
		target, _ := os.Readlink(livePath)
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(livePath), target)
		}
		if _, err := os.Stat(target); err == nil {
			t.Errorf("expected symlink %s to be stale after v2→v1 migration", rel)
		}
	}

	// --- Step 3: Doctor --fix repairs symlinks after v2→v1 ---
	fixReport, err := svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor --fix after v2→v1: %v", err)
	}
	if !fixReport.BrokenSymlinkFix {
		t.Error("expected BrokenSymlinkFix=true after v2→v1 migration")
	}

	// Symlinks should now point to v1 storage (repo root).
	for rel := range files {
		testhelpers.AssertSymlink(t,
			filepath.Join(home, rel),
			filepath.Join(repoPath, rel),
		)
	}

	// File contents should be intact.
	for rel, want := range files {
		content, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatalf("ReadFile %s after v2→v1 Doctor fix: %v", rel, err)
		}
		if string(content) != want {
			t.Errorf("content of %s = %q, want %q", rel, string(content), want)
		}
	}

	// Doctor scan should be clean.
	cleanReport, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor scan after v2→v1 fix: %v", err)
	}
	if cleanReport.HasIssues() {
		t.Error("expected no issues after Doctor --fix (v2→v1)")
	}

	// --- Step 4: Format v1→v2 ---
	result, err = svc.Format(context.Background(), false, true)
	if err != nil {
		t.Fatalf("Format v1→v2: %v", err)
	}
	if !strings.Contains(result, "formatted") {
		t.Errorf("Format result = %q, want mention of formatted", result)
	}

	// Verify v2 storage layout — files should be under common.lnk/.
	for rel := range files {
		v2Path := filepath.Join(repoPath, "common.lnk", rel)
		v1Path := filepath.Join(repoPath, rel)
		if !testhelpers.FileExists(t, v2Path) {
			t.Errorf("expected file at v2 storage path %q", v2Path)
		}
		if testhelpers.FileExists(t, v1Path) {
			t.Errorf("expected file removed from v1 storage path %q", v1Path)
		}
	}

	// Verify marker updated to v2.
	marker, err = os.ReadFile(filepath.Join(repoPath, ".lnkrepo"))
	if err != nil {
		t.Fatalf("read marker after v1→v2: %v", err)
	}
	if !strings.Contains(string(marker), "version=2") {
		t.Errorf("marker = %q, want version=2", string(marker))
	}

	// Symlinks should be stale again after v1→v2 migration.
	for rel := range files {
		livePath := filepath.Join(home, rel)
		info, err := os.Lstat(livePath)
		if err != nil {
			t.Fatalf("Lstat %s after v1→v2: %v", rel, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should still be a symlink (stale) after v1→v2 Format", rel)
		}
		target, _ := os.Readlink(livePath)
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(livePath), target)
		}
		if _, err := os.Stat(target); err == nil {
			t.Errorf("expected symlink %s to be stale after v1→v2 migration", rel)
		}
	}

	// --- Step 5: Doctor --fix repairs symlinks after v1→v2 ---
	fixReport, err = svc.Doctor(context.Background(), "", false, true, false)
	if err != nil {
		t.Fatalf("Doctor --fix after v1→v2: %v", err)
	}
	if !fixReport.BrokenSymlinkFix {
		t.Error("expected BrokenSymlinkFix=true after v1→v2 migration")
	}

	// Symlinks should now point to v2 storage.
	for rel := range files {
		testhelpers.AssertSymlink(t,
			filepath.Join(home, rel),
			filepath.Join(repoPath, "common.lnk", rel),
		)
	}

	// File contents should still be intact after full round-trip.
	for rel, want := range files {
		content, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatalf("ReadFile %s after v1→v2 Doctor fix: %v", rel, err)
		}
		if string(content) != want {
			t.Errorf("content of %s = %q, want %q after round-trip", rel, string(content), want)
		}
	}

	// Final Doctor scan should be clean.
	cleanReport, err = svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor scan after v1→v2 fix: %v", err)
	}
	if cleanReport.HasIssues() {
		t.Error("expected no issues after Doctor --fix (v1→v2)")
	}

	// --- Step 6: Verify tracker contents survived the round-trip ---
	for rel := range files {
		testhelpers.AssertTracked(t, repoPath, rel)
	}
}
