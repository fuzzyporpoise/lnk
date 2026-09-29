//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// TestIntegration_MultiMachine simulates the primary use case of lnk across
// two machines: machine A initialises, tracks files, and pushes; machine B
// clones, restores, and verifies its symlinks are in place.
func TestIntegration_MultiMachine(t *testing.T) {
	// --- Machine A: set up and push ---
	homeA := t.TempDir()
	t.Setenv("HOME", homeA)
	t.Setenv("XDG_CONFIG_HOME", "")

	remote := testhelpers.NewBareRemote(t)

	repoPathA := filepath.Join(homeA, ".config", "lnk")
	svcA := service.New(repoPathA)
	if err := svcA.Init(context.Background()); err != nil {
		t.Fatalf("machine A Init: %v", err)
	}

	// Add a remote and track some files.
	if err := testhelpers.RunGit(t, repoPathA, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("add remote: %v", err)
	}

	files := map[string]string{
		".bashrc":            "# bashrc",
		".vimrc":             "\" vimrc",
		".config/git/config": "[user]",
	}
	for rel, content := range files {
		testhelpers.MakeFile(t, filepath.Join(homeA, rel), content)
	}

	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, filepath.Join(homeA, rel))
	}
	if err := svcA.Add(context.Background(), "", paths); err != nil {
		t.Fatalf("machine A Add: %v", err)
	}

	if err := svcA.Push(context.Background()); err != nil {
		t.Fatalf("machine A Push: %v", err)
	}

	// --- Machine B: clone and restore ---
	homeB := t.TempDir()
	t.Setenv("HOME", homeB)

	repoPathB := filepath.Join(homeB, ".config", "lnk")
	svcB := service.New(repoPathB)

	if _, err := svcB.Clone(context.Background(), remote, false, nil, nil, nil); err != nil {
		t.Fatalf("machine B Clone: %v", err)
	}

	info, err := svcB.Restore(context.Background(), "", false)
	if err != nil {
		t.Fatalf("machine B Restore: %v", err)
	}

	if len(info.Restored) != len(files) {
		t.Errorf("machine B Restored = %v, want %d paths", info.Restored, len(files))
	}

	// Verify symlinks on machine B point to the cloned repo storage.
	for rel := range files {
		livePath := filepath.Join(homeB, rel)
		storagePath := filepath.Join(repoPathB, "common.lnk", rel)
		testhelpers.AssertSymlink(t, livePath, storagePath)
	}

	// Verify file contents are correct.
	for rel, want := range files {
		content, err := os.ReadFile(filepath.Join(homeB, rel))
		if err != nil {
			t.Fatalf("machine B ReadFile %s: %v", rel, err)
		}
		if string(content) != want {
			t.Errorf("machine B content of %s = %q, want %q", rel, string(content), want)
		}
	}

	// --- Machine A: add another file, push; Machine B: update ---
	t.Setenv("HOME", homeA)
	newFile := filepath.Join(homeA, ".tmux.conf")
	testhelpers.MakeFile(t, newFile, "# tmux")

	if err := svcA.Add(context.Background(), "", []string{newFile}); err != nil {
		t.Fatalf("machine A Add new file: %v", err)
	}
	if err := svcA.Push(context.Background()); err != nil {
		t.Fatalf("machine A Push new file: %v", err)
	}

	t.Setenv("HOME", homeB)
	updateInfo, err := svcB.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("machine B Update: %v", err)
	}

	// The new file should have been restored on machine B.
	newLivePath := filepath.Join(homeB, ".tmux.conf")
	newStoragePath := filepath.Join(repoPathB, "common.lnk", ".tmux.conf")
	testhelpers.AssertSymlink(t, newLivePath, newStoragePath)

	if len(updateInfo.Restored) != 1 || updateInfo.Restored[0] != ".tmux.conf" {
		t.Errorf("machine B Update Restored = %v, want [.tmux.conf]", updateInfo.Restored)
	}
}
