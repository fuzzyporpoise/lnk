package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// ---------- Bootstrap tests ----------

func TestBootstrap_ScriptNotFound(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	ran, err := svc.Bootstrap(context.Background(), os.Stdout, os.Stderr, os.Stdin)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if ran {
		t.Error("expected ran=false when no bootstrap.sh present")
	}
}

func TestBootstrap_ScriptFoundAndRuns(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Write a sentinel file to a path outside the repo so we can confirm the
	// script ran without depending on repo state.
	sentinel := filepath.Join(t.TempDir(), "bootstrap_ran")
	script := "#!/bin/sh\ntouch " + sentinel + "\n"
	scriptPath := filepath.Join(repoPath, "bootstrap.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ran, err := svc.Bootstrap(context.Background(), os.Stdout, os.Stderr, os.Stdin)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !ran {
		t.Error("expected ran=true when bootstrap.sh present")
	}
	if !testhelpers.FileExists(t, sentinel) {
		t.Error("expected sentinel file created by bootstrap.sh")
	}
}

func TestBootstrap_ScriptFails(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	script := "#!/bin/sh\nexit 1\n"
	scriptPath := filepath.Join(repoPath, "bootstrap.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ran, err := svc.Bootstrap(context.Background(), os.Stdout, os.Stderr, os.Stdin)
	if err == nil {
		t.Fatal("expected error when bootstrap.sh exits non-zero, got nil")
	}
	// ran=true because the script was found and attempted, even though it failed.
	if !ran {
		t.Error("expected ran=true even when script fails")
	}
}

func TestBootstrap_UninitializedRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	repoPath := filepath.Join(home, ".config", "lnk")
	svc := service.New(repoPath)

	_, err := svc.Bootstrap(context.Background(), os.Stdout, os.Stderr, os.Stdin)
	if err == nil {
		t.Fatal("expected error for uninitialized repo, got nil")
	}
}
