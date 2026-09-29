package service_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

func TestRestoreHook_CreatesMissingAndReportsCollisions(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	collisionPath := filepath.Join(home, ".vimrc")
	testhelpers.MakeFile(t, collisionPath, "# local vimrc")
	if err := os.WriteFile(filepath.Join(repoPath, ".lnk.common"), []byte(".bashrc\n.vimrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "common.lnk", ".vimrc"), []byte("# stored vimrc"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := svc.RestoreHook(context.Background())
	if err != nil {
		t.Fatalf("RestoreHook: %v", err)
	}
	if len(info.Restored) != 1 || info.Restored[0] != ".bashrc" {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
	if len(info.Collisions) != 1 || info.Collisions[0] != ".vimrc" {
		t.Errorf("Collisions = %v, want [.vimrc]", info.Collisions)
	}
	if len(info.BackedUp) != 0 {
		t.Errorf("BackedUp = %v, want []", info.BackedUp)
	}

	testhelpers.AssertSymlink(t, livePath, storagePath)
	if _, err := os.Lstat(collisionPath + ".lnk-backup"); err == nil {
		t.Error("expected no .lnk-backup for hook collision")
	}
	content, err := os.ReadFile(collisionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# local vimrc" {
		t.Errorf("collision file content = %q, want unchanged", string(content))
	}
}

func TestRestoreHook_Idempotent(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	storagePath, livePath := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	info, err := svc.RestoreHook(context.Background())
	if err != nil {
		t.Fatalf("RestoreHook: %v", err)
	}
	if len(info.Restored) != 0 {
		t.Errorf("Restored = %v, want []", info.Restored)
	}
	if len(info.Collisions) != 0 {
		t.Errorf("Collisions = %v, want []", info.Collisions)
	}

	testhelpers.AssertSymlink(t, livePath, storagePath)
}

func TestRunHook_PostCheckout_CreatesMissingSymlinks(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)
	t.Chdir(repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	todoLive := filepath.Join(repoDir, ".todo", "a.md")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	todoStorage := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".todo", "a.md")
	if err := os.Remove(todoLive); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := svc.RunHook(context.Background(), "post-checkout", []string{"0", "1", "1"}, &out, &errOut); err != nil {
		t.Fatalf("RunHook: %v", err)
	}

	if out.String() == "" {
		t.Error("expected hook output for restored symlink")
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %s", errOut.String())
	}
	testhelpers.AssertSymlink(t, todoLive, todoStorage)
}

func TestRunHook_PostCheckout_Idempotent(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)
	t.Chdir(repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	todoLive := filepath.Join(repoDir, ".todo", "a.md")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := svc.RunHook(context.Background(), "post-checkout", []string{"0", "1", "1"}, &out, &errOut); err != nil {
		t.Fatalf("RunHook: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected no output for idempotent hook, got %q", out.String())
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %s", errOut.String())
	}
}

func TestRunHook_PostCheckout_ReportsCollisions(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)
	t.Chdir(repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	todoLive := filepath.Join(repoDir, ".todo", "a.md")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if err := os.Remove(todoLive); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, todoLive, "local todo\n")

	var out, errOut bytes.Buffer
	if err := svc.RunHook(context.Background(), "post-checkout", []string{"0", "1", "1"}, &out, &errOut); err != nil {
		t.Fatalf("RunHook: %v", err)
	}

	if out.String() == "" {
		t.Error("expected hook output for collision")
	}
	if _, err := os.Lstat(todoLive + ".lnk-backup"); err == nil {
		t.Error("expected no .lnk-backup for hook collision")
	}
}

func TestRunHook_UnknownHook(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	var out, errOut bytes.Buffer
	if err := svc.RunHook(context.Background(), "pre-commit", nil, &out, &errOut); err != nil {
		t.Fatalf("RunHook should never return error: %v", err)
	}
	if errOut.String() == "" {
		t.Error("expected warning on stderr for unknown hook")
	}
}
