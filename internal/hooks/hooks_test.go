package hooks_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/hooks"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

func TestInstallLnkRepo_WritesPostMergeHook(t *testing.T) {
	dir := t.TempDir()
	if err := hooks.InstallLnkRepo(dir, "/usr/local/bin/lnk"); err != nil {
		t.Fatalf("InstallLnkRepo: %v", err)
	}

	hookPath := filepath.Join(dir, ".git", "hooks", "post-merge")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("stat hook: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("expected hook to be executable")
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !hooks.IsInstalled(filepath.Join(dir, ".git", "hooks"), hooks.LnkRepoHook) {
		t.Error("expected IsInstalled to return true")
	}
	if !strings.Contains(content, "hooks run post-merge") {
		t.Errorf("hook content missing expected command: %s", content)
	}
}

func TestInstallProject_WritesPostCheckoutHook(t *testing.T) {
	repoRoot := t.TempDir()
	if err := hooks.InstallProject(repoRoot, "/usr/local/bin/lnk"); err != nil {
		t.Fatalf("InstallProject: %v", err)
	}

	hookPath := filepath.Join(repoRoot, ".git", "hooks", "post-checkout")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("stat hook: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hooks run post-checkout") {
		t.Errorf("hook content missing expected command: %s", string(data))
	}
}

func TestInstall_RefusesForeignHook(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "post-merge")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho foreign\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := hooks.InstallLnkRepo(dir, "/usr/local/bin/lnk")
	if err == nil {
		t.Fatal("expected error when foreign hook exists")
	}
	if !errors.Is(err, lnkerror.ErrForeignHook) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrForeignHook)
	}
}

func TestInstall_ReplacesOwnHook(t *testing.T) {
	dir := t.TempDir()
	if err := hooks.InstallLnkRepo(dir, "/old/lnk"); err != nil {
		t.Fatalf("InstallLnkRepo: %v", err)
	}
	if err := hooks.InstallLnkRepo(dir, "/new/lnk"); err != nil {
		t.Fatalf("InstallLnkRepo second time: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", "post-merge"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/new/lnk") {
		t.Errorf("hook did not update binary path: %s", string(data))
	}
}

func TestUninstall_RemovesOwnHook(t *testing.T) {
	dir := t.TempDir()
	if err := hooks.InstallLnkRepo(dir, "/usr/local/bin/lnk"); err != nil {
		t.Fatalf("InstallLnkRepo: %v", err)
	}
	if err := hooks.UninstallLnkRepo(dir); err != nil {
		t.Fatalf("UninstallLnkRepo: %v", err)
	}

	if hooks.IsInstalled(filepath.Join(dir, ".git", "hooks"), hooks.LnkRepoHook) {
		t.Error("expected hook to be uninstalled")
	}
}

func TestUninstall_LeavesForeignHook(t *testing.T) {
	dir := t.TempDir()
	hookPath := filepath.Join(dir, ".git", "hooks", "post-merge")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho foreign\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := hooks.UninstallLnkRepo(dir); err != nil {
		t.Fatalf("UninstallLnkRepo: %v", err)
	}

	if _, err := os.Stat(hookPath); err != nil {
		t.Error("expected foreign hook to remain")
	}
}

func TestUninstall_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := hooks.UninstallLnkRepo(dir); err != nil {
		t.Fatalf("UninstallLnkRepo on missing hooks: %v", err)
	}
}
