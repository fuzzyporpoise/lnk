package gitboundary_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
)

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

func TestResolveGitRoot_InsideRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	root, err := gitboundary.ResolveGitRoot(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveGitRoot: %v", err)
	}

	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ResolveGitRoot = %q, want %q", got, want)
	}
}

func TestResolveGitRoot_OutsideRepo(t *testing.T) {
	dir := t.TempDir()

	root, err := gitboundary.ResolveGitRoot(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveGitRoot: %v", err)
	}
	if root != "" {
		t.Errorf("ResolveGitRoot = %q, want empty string", root)
	}
}

func TestResolveGitRoot_NestedDirectory(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	root, err := gitboundary.ResolveGitRoot(context.Background(), nested)
	if err != nil {
		t.Fatalf("ResolveGitRoot: %v", err)
	}

	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ResolveGitRoot = %q, want %q", got, want)
	}
}

func TestIsInsideGitRepo_Inside(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	file := filepath.Join(dir, "readme.md")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	inside, root, err := gitboundary.IsInsideGitRepo(context.Background(), file)
	if err != nil {
		t.Fatalf("IsInsideGitRepo: %v", err)
	}
	if !inside {
		t.Errorf("IsInsideGitRepo = false, want true")
	}

	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("git root = %q, want %q", got, want)
	}
}

func TestIsInsideGitRepo_Outside(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "readme.md")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	inside, root, err := gitboundary.IsInsideGitRepo(context.Background(), file)
	if err != nil {
		t.Fatalf("IsInsideGitRepo: %v", err)
	}
	if inside {
		t.Errorf("IsInsideGitRepo = true, want false")
	}
	if root != "" {
		t.Errorf("git root = %q, want empty string", root)
	}
}

func TestIsInsideGitRepo_MissingPath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")

	_, _, err := gitboundary.IsInsideGitRepo(context.Background(), missing)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}
