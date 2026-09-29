package resolver_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/resolver"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "https_with_user_capitalization_and_dotgit",
			url:  "https://github.com/User/Repo.git",
			want: "github.com/user/repo",
		},
		{
			name: "ssh_shorthand",
			url:  "git@github.com:User/Repo.git",
			want: "github.com/user/repo",
		},
		{
			name: "https_with_token",
			url:  "https://token@github.com/User/Repo.git",
			want: "github.com/user/repo",
		},
		{
			name: "ssh_url_with_port_and_subgroup",
			url:  "ssh://git@gitlab.company.com:8443/org/subgroup/project.git",
			want: "gitlab.company.com/org/subgroup/project",
		},
		{
			name: "no_dotgit_suffix",
			url:  "git@github.com:User/Repo",
			want: "github.com/user/repo",
		},
		{
			name: "ssh_shorthand_without_user",
			url:  "github.com:User/Repo.git",
			want: "github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.NormalizeRemoteURL(tt.url)
			if got != tt.want {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestResolveProjectID_WithOrigin(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	setOrigin(t, dir, "git@github.com:User/Repo.git")

	id, err := resolver.ResolveProjectID(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	if id != "github.com/user/repo" {
		t.Errorf("ResolveProjectID = %q, want github.com/user/repo", id)
	}
}

func TestResolveProjectID_NoOrigin(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	_, err := resolver.ResolveProjectID(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for repo without origin")
	}
	if !errors.Is(err, resolver.ErrNoOrigin) {
		t.Errorf("error = %v, want %v", err, resolver.ErrNoOrigin)
	}
}

func TestLocalProjectID(t *testing.T) {
	dir := t.TempDir()

	id := resolver.LocalProjectID(dir)
	base := strings.ToLower(filepath.Base(dir))
	if !strings.HasPrefix(id, "local/"+base+"-") {
		t.Errorf("LocalProjectID = %q, want local/%s-<hash> prefix", id, base)
	}

	again := resolver.LocalProjectID(dir)
	if id != again {
		t.Errorf("LocalProjectID not deterministic: %q vs %q", id, again)
	}

	other := resolver.LocalProjectID(filepath.Join(dir, "other"))
	if id == other {
		t.Errorf("expected distinct IDs for distinct roots, both %q", id)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "init", "-b", "main"},
		{"git", "-C", dir, "config", "user.email", "test@lnk"},
		{"git", "-C", dir, "config", "user.name", "Lnk Test"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}

func setOrigin(t *testing.T, dir, remoteURL string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", remoteURL).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	// Create and commit a file so the repo has a HEAD; some git versions
	// require this for remote operations.
	file := filepath.Join(dir, "README.md")
	if err := os.WriteFile(file, []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "init"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}
