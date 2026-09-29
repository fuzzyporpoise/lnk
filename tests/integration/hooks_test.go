//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/hooks"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// TestIntegration_HookPostCheckout_RestoresMissingSymlinks verifies that the
// installed post-checkout hook recreates project-scope symlinks after a git
// checkout without backing up existing files.
func buildLnkBinary(t *testing.T) string {
	t.Helper()
	lnkPath := filepath.Join(t.TempDir(), "lnk")
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", lnkPath, ".")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build lnk binary: %v\n%s", err, out)
	}
	return lnkPath
}

func TestIntegration_HookPostCheckout_RestoresMissingSymlinks(t *testing.T) {
	lnkBinary := buildLnkBinary(t)

	svc, home := testhelpers.TestHome(t)
	projectDir := filepath.Join(home, "project")
	testhelpers.MakeDir(t, projectDir)
	testhelpers.InitGitRepo(t, projectDir)

	if out, err := exec.Command("git", "-C", projectDir, "remote", "add", "origin", "git@github.com:User/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	readme := filepath.Join(projectDir, "README.md")
	testhelpers.MakeFile(t, readme, "# repo\n")
	if out, err := exec.Command("git", "-C", projectDir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", projectDir, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), projectDir, ".todo/**"); err != nil {
		t.Fatalf("ProjectAddPattern: %v", err)
	}

	todoLive := filepath.Join(projectDir, ".todo", "a.md")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	result, err := ps.ProjectPush(context.Background(), projectDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	todoStorage := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".todo", "a.md")
	testhelpers.AssertSymlink(t, todoLive, todoStorage)

	// Install the post-checkout hook using a real lnk binary.
	if err := hooks.InstallProject(projectDir, lnkBinary); err != nil {
		t.Fatalf("InstallProject: %v", err)
	}

	// Create a branch so we can switch back to main and trigger the hook.
	if out, err := exec.Command("git", "-C", projectDir, "checkout", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b feature: %v\n%s", err, out)
	}

	// Remove the symlink to simulate a fresh clone / checkout state.
	if err := os.Remove(todoLive); err != nil {
		t.Fatal(err)
	}

	// Trigger the hook by switching back to main.
	out, err := exec.Command("git", "-C", projectDir, "checkout", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("git checkout main: %v\n%s", err, out)
	}

	// The hook should have recreated the symlink.
	testhelpers.AssertSymlink(t, todoLive, todoStorage)
	if _, err := os.Lstat(todoLive + ".lnk-backup"); err == nil {
		t.Error("expected no .lnk-backup from hook restore")
	}
}

// TestIntegration_HookPostCheckout_ReportsCollision verifies that the
// post-checkout hook leaves a real file in place and reports the collision.
func TestIntegration_HookPostCheckout_ReportsCollision(t *testing.T) {
	lnkBinary := buildLnkBinary(t)

	svc, home := testhelpers.TestHome(t)
	projectDir := filepath.Join(home, "project")
	testhelpers.MakeDir(t, projectDir)
	testhelpers.InitGitRepo(t, projectDir)

	if out, err := exec.Command("git", "-C", projectDir, "remote", "add", "origin", "git@github.com:User/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	readme := filepath.Join(projectDir, "README.md")
	testhelpers.MakeFile(t, readme, "# repo\n")
	if out, err := exec.Command("git", "-C", projectDir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", projectDir, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), projectDir, ".todo/**"); err != nil {
		t.Fatalf("ProjectAddPattern: %v", err)
	}

	todoLive := filepath.Join(projectDir, ".todo", "a.md")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	if _, err := ps.ProjectPush(context.Background(), projectDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	// Replace the symlink with a real file to create a collision.
	if err := os.Remove(todoLive); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, todoLive, "local todo\n")

	if err := hooks.InstallProject(projectDir, lnkBinary); err != nil {
		t.Fatalf("InstallProject: %v", err)
	}

	if out, err := exec.Command("git", "-C", projectDir, "checkout", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b feature: %v\n%s", err, out)
	}

	out, err := exec.Command("git", "-C", projectDir, "checkout", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("git checkout main: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "collision") {
		t.Errorf("expected collision message in hook output, got:\n%s", out)
	}

	content, err := os.ReadFile(todoLive)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "local todo\n" {
		t.Errorf("collision file was modified: %q", string(content))
	}
	if _, err := os.Lstat(todoLive + ".lnk-backup"); err == nil {
		t.Error("expected no .lnk-backup from hook collision")
	}
}

// TestIntegration_HookPostCheckout_Idempotent verifies that a branch switch
// with already-correct symlinks is a no-op.
func TestIntegration_HookPostCheckout_Idempotent(t *testing.T) {
	lnkBinary := buildLnkBinary(t)

	svc, home := testhelpers.TestHome(t)
	projectDir := filepath.Join(home, "project")
	testhelpers.MakeDir(t, projectDir)
	testhelpers.InitGitRepo(t, projectDir)

	if out, err := exec.Command("git", "-C", projectDir, "remote", "add", "origin", "git@github.com:User/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	readme := filepath.Join(projectDir, "README.md")
	testhelpers.MakeFile(t, readme, "# repo\n")
	if out, err := exec.Command("git", "-C", projectDir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", projectDir, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), projectDir, ".todo/**"); err != nil {
		t.Fatalf("ProjectAddPattern: %v", err)
	}

	todoLive := filepath.Join(projectDir, ".todo", "a.md")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	result, err := ps.ProjectPush(context.Background(), projectDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	todoStorage := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".todo", "a.md")

	if err := hooks.InstallProject(projectDir, lnkBinary); err != nil {
		t.Fatalf("InstallProject: %v", err)
	}

	if out, err := exec.Command("git", "-C", projectDir, "checkout", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b feature: %v\n%s", err, out)
	}

	out, err := exec.Command("git", "-C", projectDir, "checkout", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("git checkout main: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "restored") {
		t.Errorf("expected no restore output for idempotent hook, got:\n%s", out)
	}

	testhelpers.AssertSymlink(t, todoLive, todoStorage)
}
