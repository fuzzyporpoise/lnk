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

// TestIntegration_Update simulates the core lnk update workflow: a second
// machine clones a repo, a first machine adds new files and pushes, then the
// second machine runs Update and verifies the new files are pulled and symlinked.
func TestIntegration_Update(t *testing.T) {
	remote := testhelpers.NewBareRemote(t)

	// --- Machine A: init, add files, push ---
	homeA := t.TempDir()
	t.Setenv("HOME", homeA)
	t.Setenv("XDG_CONFIG_HOME", "")

	repoPathA := filepath.Join(homeA, ".config", "lnk")
	svcA := service.New(repoPathA)
	if err := svcA.Init(context.Background()); err != nil {
		t.Fatalf("machine A Init: %v", err)
	}
	if err := testhelpers.RunGit(t, repoPathA, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("add remote: %v", err)
	}

	initialFiles := map[string]string{
		".bashrc": "# bashrc",
		".vimrc":  "\" vimrc",
	}
	for rel, content := range initialFiles {
		testhelpers.MakeFile(t, filepath.Join(homeA, rel), content)
	}
	initialPaths := make([]string, 0, len(initialFiles))
	for rel := range initialFiles {
		initialPaths = append(initialPaths, filepath.Join(homeA, rel))
	}
	if err := svcA.Add(context.Background(), "", initialPaths); err != nil {
		t.Fatalf("machine A Add initial files: %v", err)
	}
	if err := svcA.Push(context.Background()); err != nil {
		t.Fatalf("machine A Push: %v", err)
	}

	// --- Machine B: clone and restore initial state ---
	homeB := t.TempDir()
	t.Setenv("HOME", homeB)

	repoPathB := filepath.Join(homeB, ".config", "lnk")
	svcB := service.New(repoPathB)
	if _, err := svcB.Clone(context.Background(), remote, false, nil, nil, nil); err != nil {
		t.Fatalf("machine B Clone: %v", err)
	}

	if _, err := svcB.Restore(context.Background(), "", false); err != nil {
		t.Fatalf("machine B Restore: %v", err)
	}

	// Verify initial symlinks on machine B.
	for rel := range initialFiles {
		testhelpers.AssertSymlink(t,
			filepath.Join(homeB, rel),
			filepath.Join(repoPathB, "common.lnk", rel),
		)
	}

	// --- Machine A: add more files and push ---
	t.Setenv("HOME", homeA)

	newFiles := map[string]string{
		".tmux.conf":         "# tmux",
		".config/git/config": "[user]",
	}
	for rel, content := range newFiles {
		testhelpers.MakeFile(t, filepath.Join(homeA, rel), content)
	}
	newPaths := make([]string, 0, len(newFiles))
	for rel := range newFiles {
		newPaths = append(newPaths, filepath.Join(homeA, rel))
	}
	if err := svcA.Add(context.Background(), "", newPaths); err != nil {
		t.Fatalf("machine A Add new files: %v", err)
	}
	if err := svcA.Push(context.Background()); err != nil {
		t.Fatalf("machine A Push new files: %v", err)
	}

	// --- Machine B: Update pulls and restores new files ---
	t.Setenv("HOME", homeB)

	info, err := svcB.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("machine B Update: %v", err)
	}

	// Only the new files should be in Restored — initial files already have
	// correct symlinks and should be skipped by Restore.
	if len(info.Restored) != len(newFiles) {
		t.Errorf("Restored = %v, want %d new files", info.Restored, len(newFiles))
	}

	// New files should be symlinked on machine B.
	for rel, want := range newFiles {
		livePath := filepath.Join(homeB, rel)
		storagePath := filepath.Join(repoPathB, "common.lnk", rel)
		testhelpers.AssertSymlink(t, livePath, storagePath)

		content, err := os.ReadFile(livePath)
		if err != nil {
			t.Fatalf("ReadFile %s on machine B: %v", rel, err)
		}
		if string(content) != want {
			t.Errorf("content of %s = %q, want %q", rel, string(content), want)
		}
	}

	// Initial files should still be correctly symlinked.
	for rel := range initialFiles {
		testhelpers.AssertSymlink(t,
			filepath.Join(homeB, rel),
			filepath.Join(repoPathB, "common.lnk", rel),
		)
	}

	// --- Machine B: Update with no new changes is a no-op ---
	infoNoOp, err := svcB.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("machine B Update no-op: %v", err)
	}
	if len(infoNoOp.Restored) != 0 {
		t.Errorf("expected no restored files on no-op Update, got %v", infoNoOp.Restored)
	}

	// --- Machine A: add host-scoped file, push ---
	t.Setenv("HOME", homeA)

	hostFile := filepath.Join(homeA, ".ssh/config")
	testhelpers.MakeFile(t, hostFile, "# ssh config")
	if err := svcA.Add(context.Background(), "testhost", []string{hostFile}); err != nil {
		t.Fatalf("machine A Add host file: %v", err)
	}
	if err := svcA.Push(context.Background()); err != nil {
		t.Fatalf("machine A Push host file: %v", err)
	}

	// --- Machine B: Update with host scope restores host-scoped file ---
	t.Setenv("HOME", homeB)

	hostInfo, err := svcB.Update(context.Background(), "testhost")
	if err != nil {
		t.Fatalf("machine B Update with host: %v", err)
	}

	if len(hostInfo.Restored) != 1 || hostInfo.Restored[0] != ".ssh/config" {
		t.Errorf("Restored = %v, want [.ssh/config]", hostInfo.Restored)
	}

	testhelpers.AssertSymlink(t,
		filepath.Join(homeB, ".ssh/config"),
		filepath.Join(repoPathB, "testhost.lnk", ".ssh/config"),
	)

	// --- Machine B: Update without host scope does not restore host-scoped file ---
	// Remove the host symlink to give Restore something to do if it incorrectly
	// includes host-scoped files without being asked.
	if err := os.Remove(filepath.Join(homeB, ".ssh/config")); err != nil {
		t.Fatalf("remove host symlink: %v", err)
	}

	noHostInfo, err := svcB.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("machine B Update without host: %v", err)
	}

	for _, restored := range noHostInfo.Restored {
		if restored == ".ssh/config" {
			t.Error("host-scoped file should not be restored when no host is specified")
		}
	}
	if testhelpers.FileExists(t, filepath.Join(homeB, ".ssh/config")) {
		t.Error("expected host-scoped symlink to remain absent without --host")
	}
}
