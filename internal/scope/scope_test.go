package scope_test

import (
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/scope"
)

func TestHomeRelativeResolver_ToStorage(t *testing.T) {
	r := &scope.HomeRelativeResolver{Home: "/home/user"}

	got, err := r.ToStorage("/home/user/.bashrc")
	if err != nil {
		t.Fatalf("ToStorage: %v", err)
	}
	if got != ".bashrc" {
		t.Errorf("ToStorage = %q, want .bashrc", got)
	}
}

func TestHomeRelativeResolver_ToStorage_OutsideHome(t *testing.T) {
	r := &scope.HomeRelativeResolver{Home: "/home/user"}

	got, err := r.ToStorage("/etc/hosts")
	if err != nil {
		t.Fatalf("ToStorage: %v", err)
	}
	if got == "hosts" || !filepath.IsAbs(got) && !containsDotDot(got) {
		t.Errorf("ToStorage = %q, expected a path outside home", got)
	}
}

func TestHomeRelativeResolver_ToLive(t *testing.T) {
	r := &scope.HomeRelativeResolver{Home: "/home/user"}

	got, err := r.ToLive(".bashrc")
	if err != nil {
		t.Fatalf("ToLive: %v", err)
	}
	want := "/home/user/.bashrc"
	if got != want {
		t.Errorf("ToLive = %q, want %q", got, want)
	}
}

func TestHomeRelativeResolver_BaseDir(t *testing.T) {
	r := &scope.HomeRelativeResolver{StorageDir: "/home/user/.config/lnk/common.lnk"}
	if got := r.BaseDir(); got != r.StorageDir {
		t.Errorf("BaseDir = %q, want %q", got, r.StorageDir)
	}
}

func TestProjectRootResolver_ToStorage(t *testing.T) {
	r := &scope.ProjectRootResolver{GitRoot: "/home/user/repos/hermes"}

	got, err := r.ToStorage("/home/user/repos/hermes/.cursor/rules.md")
	if err != nil {
		t.Fatalf("ToStorage: %v", err)
	}
	want := filepath.Join(".cursor", "rules.md")
	if got != want {
		t.Errorf("ToStorage = %q, want %q", got, want)
	}
}

func TestProjectRootResolver_ToStorage_OutsideRoot(t *testing.T) {
	r := &scope.ProjectRootResolver{GitRoot: "/home/user/repos/hermes"}

	got, err := r.ToStorage("/home/user/.bashrc")
	if err != nil {
		t.Fatalf("ToStorage: %v", err)
	}
	if got == ".bashrc" || !containsDotDot(got) {
		t.Errorf("ToStorage = %q, expected a path outside project root", got)
	}
}

func TestProjectRootResolver_ToLive(t *testing.T) {
	r := &scope.ProjectRootResolver{GitRoot: "/home/user/repos/hermes"}

	got, err := r.ToLive(".cursor/rules.md")
	if err != nil {
		t.Fatalf("ToLive: %v", err)
	}
	want := filepath.Join("/home/user/repos/hermes", ".cursor", "rules.md")
	if got != want {
		t.Errorf("ToLive = %q, want %q", got, want)
	}
}

func TestProjectRootResolver_BaseDir(t *testing.T) {
	r := &scope.ProjectRootResolver{StorageDir: "/home/user/.config/lnk/projects/github.com/user/repo"}
	if got := r.BaseDir(); got != r.StorageDir {
		t.Errorf("BaseDir = %q, want %q", got, r.StorageDir)
	}
}

func containsDotDot(s string) bool {
	return strings.Contains(s, "..")
}
