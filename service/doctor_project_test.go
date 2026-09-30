package service_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

func TestDoctor_ProjectIssues_OrphanedStorage(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Create a stored project that has no live repo on disk.
	orphanedStorage := filepath.Join(repoPath, "projects", "github.com", "alice", "orphaned")
	if err := os.MkdirAll(orphanedStorage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanedStorage, ".lnkproject"), []byte("github.com/alice/orphaned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanedStorage, "config"), []byte("config"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !report.HasIssues() {
		t.Fatal("expected doctor to report issues")
	}
	if !hasProjectIssue(report.ProjectIssues, "github.com/alice/orphaned", "project registry") {
		t.Errorf("ProjectIssues = %v, expected cache issue for orphaned storage", report.ProjectIssues)
	}
	_ = home
}

func TestDoctor_ProjectIssues_BrokenSymlink(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	projectDir := filepath.Join(home, "projects", "myapp")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testhelpers.InitGitRepo(t, projectDir)
	if out, err := execGit(t, projectDir, "remote", "add", "origin", "git@github.com:alice/myapp.git"); err != nil {
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

	// Break the symlink by removing its storage target.
	storageTarget := filepath.Join(repoPath, "projects", "github.com", "alice", "myapp", "agents.md")
	if err := os.Remove(storageTarget); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !hasProjectIssue(report.ProjectIssues, "github.com/alice/myapp", "broken symlink") {
		t.Errorf("ProjectIssues = %v, expected broken symlink issue", report.ProjectIssues)
	}
}

func TestDoctor_ProjectIssues_NoIssues(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	projectDir := filepath.Join(home, "projects", "healthy")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testhelpers.InitGitRepo(t, projectDir)
	if out, err := execGit(t, projectDir, "remote", "add", "origin", "git@github.com:alice/healthy.git"); err != nil {
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
	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{filepath.Dir(projectDir)}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}

	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	for _, issue := range report.ProjectIssues {
		if issue.ProjectID == "github.com/alice/healthy" {
			t.Fatalf("unexpected project issue for healthy project: %v", issue)
		}
	}
}

func hasProjectIssue(issues []service.ProjectIssue, projectID, issueSubstr string) bool {
	return slices.ContainsFunc(issues, func(i service.ProjectIssue) bool {
		return i.ProjectID == projectID && strings.Contains(i.Issue, issueSubstr)
	})
}

func TestDoctor_ProjectIssues_CacheMissing(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "projects", "myapp")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testhelpers.InitGitRepo(t, repoDir)
	if out, err := execGit(t, repoDir, "remote", "add", "origin", "git@github.com:alice/myapp.git"); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "config"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config"), []byte("config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	// ProjectPush records the cache; clear it to verify the doctor warning.
	if err := os.Remove(service.RegistryPath()); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clear cache: %v", err)
	}

	// No cache entry exists yet, so doctor reports the project as uncached.
	report, err := svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !hasProjectIssue(report.ProjectIssues, "github.com/alice/myapp", "project registry") {
		t.Errorf("expected cache issue, got %v", report.ProjectIssues)
	}

	// Repair via project cache --scan.
	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{filepath.Dir(repoDir)}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}
	report, err = svc.Doctor(context.Background(), "", false, false, false)
	if err != nil {
		t.Fatalf("Doctor after cache repair: %v", err)
	}
	if hasProjectIssue(report.ProjectIssues, "github.com/alice/myapp", "project registry") {
		t.Errorf("expected cache issue to be repaired, got %v", report.ProjectIssues)
	}
}
