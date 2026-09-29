package cmd_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/cmd"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

func TestCreateCmd_MixedFileAndDir(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	_ = svc

	root := cmd.NewRootCommand()
	root.SetArgs([]string{
		"create",
		filepath.Join(home, ".config", "awesome") + string(filepath.Separator),
		filepath.Join(home, ".bashrc"),
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for mixed file/directory create, got nil")
	}
	if !errors.Is(err, lnkerror.ErrMixedCreateTypes) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrMixedCreateTypes)
	}
}

func TestCreateCmd_DirFlagCreatesDirectory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	root := cmd.NewRootCommand()
	root.SetArgs([]string{
		"create",
		"--dir",
		filepath.Join(home, ".config", "awesome"),
	})

	var buf bytes.Buffer
	root.SetOut(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Created and tracked 1 dir(s)") {
		t.Errorf("unexpected output: %q", out)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "awesome")
	livePath := filepath.Join(home, ".config", "awesome")
	testhelpers.AssertSymlink(t, livePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/awesome")
}

func TestCreateCmd_TrailingSlashCreatesDirectory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	root := cmd.NewRootCommand()
	root.SetArgs([]string{
		"create",
		filepath.Join(home, ".config", "awesome") + string(filepath.Separator),
	})

	var buf bytes.Buffer
	root.SetOut(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Created and tracked 1 dir(s)") {
		t.Errorf("unexpected output: %q", out)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "awesome")
	livePath := filepath.Join(home, ".config", "awesome")
	testhelpers.AssertSymlink(t, livePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/awesome")
}

func TestCreateCmd_MultipleDirsWithTrailingSlash(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	root := cmd.NewRootCommand()
	root.SetArgs([]string{
		"create",
		filepath.Join(home, ".config", "awesome") + string(filepath.Separator),
		filepath.Join(home, ".config", "other") + string(filepath.Separator),
	})

	var buf bytes.Buffer
	root.SetOut(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Created and tracked 2 dir(s)") {
		t.Errorf("unexpected output: %q", out)
	}

	testhelpers.AssertTracked(t, repoPath, ".config/awesome")
	testhelpers.AssertTracked(t, repoPath, ".config/other")
}

func TestCreateCmd_FileOutput(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	root := cmd.NewRootCommand()
	root.SetArgs([]string{
		"create",
		filepath.Join(home, ".bashrc"),
	})

	var buf bytes.Buffer
	root.SetOut(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Created and tracked 1 file(s)") {
		t.Errorf("unexpected output: %q", out)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, filepath.Join(home, ".bashrc"), storagePath)
	testhelpers.AssertTracked(t, repoPath, ".bashrc")
}
