package service_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// ---------- Init tests ----------

func TestInit_FreshDirectory(t *testing.T) {
	svc, repoPath := testhelpers.NewTestRepo(t)
	testhelpers.InitRepo(t, svc)

	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".git")) {
		t.Error("expected .git directory to exist")
	}
	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".lnkrepo")) {
		t.Error("expected .lnkrepo marker to exist")
	}

	content, err := os.ReadFile(filepath.Join(repoPath, ".lnkrepo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "version=2") {
		t.Errorf("marker content = %q, want version=2", string(content))
	}

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d: %v", len(commits), commits)
	}
	if !strings.Contains(commits[0], "initialize repository") {
		t.Errorf("commit message = %q, want 'initialize repository'", commits[0])
	}
}

func TestInit_Idempotent(t *testing.T) {
	svc, repoPath := testhelpers.NewTestRepo(t)
	testhelpers.InitRepo(t, svc)

	// Second call should succeed without creating a new commit.
	if err := svc.Init(context.Background()); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 1 {
		t.Errorf("expected 1 commit after double init, got %d", len(commits))
	}
}

func TestInit_ExistingGitRepoWithoutMarker(t *testing.T) {
	_, repoPath := testhelpers.NewTestRepo(t)

	// Create a git repo without the lnk marker.
	if out, err := exec.Command("git", "-C", repoPath, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	svc := service.New(repoPath)
	err := svc.Init(context.Background())
	if err == nil {
		t.Fatal("expected error for existing git repo without lnk marker, got nil")
	}
	if !errors.Is(err, lnkerror.ErrGitRepoExists) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrGitRepoExists)
	}
}

// ---------- Clone tests ----------

func TestClone_WithoutBootstrap(t *testing.T) {
	remote := testhelpers.NewBareRemote(t)
	testhelpers.PushInitialCommit(t, remote)

	dest := filepath.Join(t.TempDir(), "cloned")
	svc := service.New(dest)

	ran, err := svc.Clone(context.Background(), remote, false, nil, nil, nil)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if ran {
		t.Error("expected ran=false when runBootstrap=false")
	}
	if !testhelpers.FileExists(t, filepath.Join(dest, ".git")) {
		t.Error("expected .git to exist after clone")
	}
	if !testhelpers.FileExists(t, filepath.Join(dest, ".lnkrepo")) {
		t.Error("expected .lnkrepo to exist after clone")
	}
}

func TestClone_WithBootstrapNoScript(t *testing.T) {
	remote := testhelpers.NewBareRemote(t)
	testhelpers.PushInitialCommit(t, remote)

	dest := filepath.Join(t.TempDir(), "cloned")
	svc := service.New(dest)

	ran, err := svc.Clone(context.Background(), remote, true, os.Stdout, os.Stderr, os.Stdin)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if ran {
		t.Error("expected ran=false when no bootstrap.sh present")
	}
}

func TestClone_WithBootstrapScript(t *testing.T) {
	remote := testhelpers.NewBareRemote(t)
	src := testhelpers.PushInitialCommit(t, remote)

	// Add a bootstrap.sh that writes a sentinel file.
	sentinel := filepath.Join(t.TempDir(), "bootstrap_ran")
	script := "#!/bin/sh\ntouch " + sentinel + "\n"
	if err := os.WriteFile(filepath.Join(src, "bootstrap.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"git", "-C", src, "add", "bootstrap.sh"},
		{"git", "-C", src, "commit", "-m", "add bootstrap"},
		{"git", "-C", src, "push"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	dest := filepath.Join(t.TempDir(), "cloned")
	svc := service.New(dest)

	ran, err := svc.Clone(context.Background(), remote, true, os.Stdout, os.Stderr, os.Stdin)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if !ran {
		t.Error("expected ran=true when bootstrap.sh present")
	}
	if !testhelpers.FileExists(t, sentinel) {
		t.Error("expected sentinel file created by bootstrap.sh")
	}
}

func TestClone_InvalidURL(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cloned")
	svc := service.New(dest)

	_, err := svc.Clone(context.Background(), "file:///nonexistent/path", false, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error cloning invalid URL, got nil")
	}
}

// ---------- ResolveRepoPath tests ----------

func TestResolveRepoPath_ExplicitPath(t *testing.T) {
	explicit := "/some/explicit/path"
	result := service.ResolveRepoPath(explicit)
	if result != explicit {
		t.Errorf("result = %q, want %q", result, explicit)
	}
}

func TestResolveRepoPath_LNK_REPO(t *testing.T) {
	t.Setenv("LNK_REPO", "/from/lnk_repo")
	t.Setenv("LNK_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	result := service.ResolveRepoPath("")
	if result != "/from/lnk_repo" {
		t.Errorf("result = %q, want /from/lnk_repo", result)
	}
}

func TestResolveRepoPath_LNK_HOME_FallbackWhenNoLNK_REPO(t *testing.T) {
	t.Setenv("LNK_REPO", "")
	t.Setenv("LNK_HOME", "/from/lnk_home")
	t.Setenv("XDG_CONFIG_HOME", "")

	result := service.ResolveRepoPath("")
	if result != "/from/lnk_home" {
		t.Errorf("result = %q, want /from/lnk_home", result)
	}
}

func TestResolveRepoPath_LNK_REPO_TakesPriorityOverLNK_HOME(t *testing.T) {
	t.Setenv("LNK_REPO", "/from/lnk_repo")
	t.Setenv("LNK_HOME", "/from/lnk_home")
	t.Setenv("XDG_CONFIG_HOME", "")

	result := service.ResolveRepoPath("")
	if result != "/from/lnk_repo" {
		t.Errorf("result = %q, want /from/lnk_repo", result)
	}
}

func TestResolveRepoPath_XDG_CONFIG_HOME(t *testing.T) {
	t.Setenv("LNK_REPO", "")
	t.Setenv("LNK_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	result := service.ResolveRepoPath("")
	want := "/custom/config/lnk"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}

func TestResolveRepoPath_Default(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LNK_REPO", "")
	t.Setenv("LNK_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	result := service.ResolveRepoPath("")
	want := filepath.Join(home, ".config", "lnk")
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}

func TestResolveRepoPath_ExplicitTakesPriorityOverEnvVars(t *testing.T) {
	t.Setenv("LNK_REPO", "/from/lnk_repo")
	t.Setenv("LNK_HOME", "/from/lnk_home")
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	explicit := "/explicit/wins"
	result := service.ResolveRepoPath(explicit)
	if result != explicit {
		t.Errorf("result = %q, want %q", result, explicit)
	}
}
