package service_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

func TestDetectProjectScope_OutsideGitRepo(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	root, id, resolver, ok, err := svc.DetectProjectScope(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("DetectProjectScope: %v", err)
	}
	if ok {
		t.Fatalf("expected no project scope outside git repo, got root=%q id=%q", root, id)
	}
	if resolver != nil {
		t.Fatal("expected nil resolver outside git repo")
	}
}

func TestDetectProjectScope_GitRepoWithoutInclude(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	projectDir := t.TempDir()
	testhelpers.InitGitRepo(t, projectDir)

	_, _, _, ok, err := svc.DetectProjectScope(context.Background(), projectDir)
	if err != nil {
		t.Fatalf("DetectProjectScope: %v", err)
	}
	if ok {
		t.Fatal("expected no project scope without .lnkinclude")
	}
}

func TestDetectProjectScope_WithInclude(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	projectDir := t.TempDir()
	testhelpers.InitGitRepo(t, projectDir)
	if out, err := execGit(t, projectDir, "remote", "add", "origin", "git@github.com:alice/project.git"); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".lnkinclude"), []byte("agents.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, id, resolver, ok, err := svc.DetectProjectScope(context.Background(), projectDir)
	if err != nil {
		t.Fatalf("DetectProjectScope: %v", err)
	}
	if !ok {
		t.Fatal("expected project scope detected")
	}
	canonicalProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if root != canonicalProjectDir {
		t.Errorf("root = %q, want %q", root, canonicalProjectDir)
	}
	if id != "github.com/alice/project" {
		t.Errorf("id = %q, want github.com/alice/project", id)
	}
	if resolver == nil {
		t.Fatal("expected non-nil resolver")
	}
}

func TestDetectProjectScope_LnkRepoNotProject(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	root, id, _, ok, err := svc.DetectProjectScope(context.Background(), svc.RepoPath())
	if err != nil {
		t.Fatalf("DetectProjectScope: %v", err)
	}
	if ok {
		t.Fatalf("expected lnk repo not to be treated as project, got root=%q id=%q", root, id)
	}
}

func TestRestoreWithProject_DetectsAndRestores(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Set up a common-scope file so Restore has non-project work to do.
	_, commonLive := setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	if err := os.Remove(commonLive); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	testhelpers.InitGitRepo(t, projectDir)
	if out, err := execGit(t, projectDir, "remote", "add", "origin", "git@github.com:alice/project.git"); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".lnkinclude"), []byte("agents.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "agents.md"), []byte("agents"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := service.NewProjectService(svc)
	if _, err := ps.ProjectPush(context.Background(), projectDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	projectLive := filepath.Join(projectDir, "agents.md")
	if err := os.Remove(projectLive); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	info, err := svc.RestoreWithProject(context.Background(), "", false, false)
	if err != nil {
		t.Fatalf("RestoreWithProject: %v", err)
	}

	if !contains(info.Restored, ".bashrc") {
		t.Errorf("Restored = %v, expected to contain .bashrc", info.Restored)
	}
	if !contains(info.Restored, "agents.md") {
		t.Errorf("Restored = %v, expected to contain agents.md", info.Restored)
	}

	testhelpers.AssertSymlink(t, commonLive, filepath.Join(repoPath, "common.lnk", ".bashrc"))
	testhelpers.AssertSymlink(t, projectLive, filepath.Join(repoPath, "projects", "github.com", "alice", "project", "agents.md"))
}

func TestRestoreWithProject_NoProjectOptOut(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	projectDir := t.TempDir()
	testhelpers.InitGitRepo(t, projectDir)
	if out, err := execGit(t, projectDir, "remote", "add", "origin", "git@github.com:alice/project.git"); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".lnkinclude"), []byte("agents.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "agents.md"), []byte("agents"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := service.NewProjectService(svc)
	if _, err := ps.ProjectPush(context.Background(), projectDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	projectLive := filepath.Join(projectDir, "agents.md")
	if err := os.Remove(projectLive); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	_, err = svc.RestoreWithProject(context.Background(), "", true, false)
	if err != nil {
		t.Fatalf("RestoreWithProject(noProject=true): %v", err)
	}

	if _, err := os.Lstat(projectLive); !os.IsNotExist(err) {
		t.Fatal("project symlink created despite --no-project")
	}
}

func TestRestoreWithProject_FromSubdirectory(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	projectDir := t.TempDir()
	testhelpers.InitGitRepo(t, projectDir)
	if out, err := execGit(t, projectDir, "remote", "add", "origin", "git@github.com:alice/project.git"); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".lnkinclude"), []byte("agents.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "agents.md"), []byte("agents"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := service.NewProjectService(svc)
	if _, err := ps.ProjectPush(context.Background(), projectDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	projectLive := filepath.Join(projectDir, "agents.md")
	if err := os.Remove(projectLive); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(projectDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	if _, err := svc.RestoreWithProject(context.Background(), "", false, false); err != nil {
		t.Fatalf("RestoreWithProject: %v", err)
	}

	testhelpers.AssertSymlink(t, projectLive, filepath.Join(svc.RepoPath(), "projects", "github.com", "alice", "project", "agents.md"))
}

func execGit(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := append([]string{"-C", dir}, args...)
	return osExec("git", cmd...)
}

func osExec(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
