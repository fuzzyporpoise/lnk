package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// ---------- Success cases ----------

func TestCreate_SingleFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	if err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".bashrc")

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) < 2 {
		t.Errorf("expected at least 2 commits (init + create), got %d", len(commits))
	}
}

func TestCreate_MultipleFiles(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".vimrc"),
	}
	if err := svc.Create(context.Background(), "", paths, service.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, p := range paths {
		rel := filepath.Base(p)
		storagePath := filepath.Join(repoPath, "common.lnk", rel)
		testhelpers.AssertSymlink(t, p, storagePath)
		testhelpers.AssertTracked(t, repoPath, rel)
	}

	commits := testhelpers.GitLog(t, repoPath)
	if len(commits) != 2 {
		t.Errorf("expected 2 commits (init + create), got %d", len(commits))
	}
}

func TestCreate_DirectoryWithDirFlag(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	dirPath := filepath.Join(home, ".config", "awesome")
	if err := svc.Create(context.Background(), "", []string{dirPath}, service.CreateOptions{AsDir: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "awesome")
	testhelpers.AssertSymlink(t, dirPath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/awesome")

	keepPath := filepath.Join(storagePath, ".lnkkeep")
	if !testhelpers.FileExists(t, keepPath) {
		t.Errorf("expected .lnkkeep at %q", keepPath)
	}
}

func TestCreate_DirectoryWithTrailingSlash(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	dirPath := filepath.Join(home, ".config", "awesome") + string(filepath.Separator)
	if err := svc.Create(context.Background(), "", []string{dirPath}, service.CreateOptions{AsDir: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "awesome")
	livePath := filepath.Join(home, ".config", "awesome")
	testhelpers.AssertSymlink(t, livePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/awesome")
}

func TestCreate_NestedFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".config", "git", "config")
	if err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "git", "config")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/git/config")
}

// ---------- Host scope tests ----------

func TestCreate_HostScope_ExplicitHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	if err := svc.Create(context.Background(), "testhost", []string{filePath}, service.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	hostStorage := filepath.Join(repoPath, "testhost.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, filePath, hostStorage)
	testhelpers.AssertTrackedInScope(t, repoPath, "testhost", ".bashrc")
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")
}

// ---------- V1 format tests ----------

func TestCreate_V1_SingleFile(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	filePath := filepath.Join(home, ".bashrc")
	if err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{}); err != nil {
		t.Fatalf("Create v1: %v", err)
	}

	storagePath := filepath.Join(repoPath, ".bashrc")
	testhelpers.AssertSymlink(t, filePath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".bashrc")
}

// ---------- Failure cases ----------

func TestCreate_ExistingFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error creating existing file, got nil")
	}
	if !errors.Is(err, lnkerror.ErrPathExists) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrPathExists)
	}
}

func TestCreate_ExistingNonEmptyDirectory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	dirPath := filepath.Join(home, ".config", "awesome")
	testhelpers.MakeDir(t, dirPath)
	testhelpers.MakeFile(t, filepath.Join(dirPath, "existing.conf"), "content")

	err := svc.Create(context.Background(), "", []string{dirPath}, service.CreateOptions{AsDir: true})
	if err == nil {
		t.Fatal("expected error creating non-empty directory, got nil")
	}
	if !errors.Is(err, lnkerror.ErrPathExists) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrPathExists)
	}
}

func TestCreate_ExistingEmptyDirectory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	dirPath := filepath.Join(home, ".config", "awesome")
	testhelpers.MakeDir(t, dirPath)

	if err := svc.Create(context.Background(), "", []string{dirPath}, service.CreateOptions{AsDir: true}); err != nil {
		t.Fatalf("Create empty dir: %v", err)
	}

	storagePath := filepath.Join(repoPath, "common.lnk", ".config", "awesome")
	testhelpers.AssertSymlink(t, dirPath, storagePath)
	testhelpers.AssertTracked(t, repoPath, ".config/awesome")
}

func TestCreate_PathIsSymlink(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	targetPath := filepath.Join(home, ".real-bashrc")
	testhelpers.MakeFile(t, targetPath, "# real bashrc")
	if err := os.Symlink(targetPath, filePath); err != nil {
		t.Fatal(err)
	}

	err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error creating over a symlink, got nil")
	}
	if !errors.Is(err, fs.ErrUnsupportedType) {
		t.Errorf("error = %v, want %v", err, fs.ErrUnsupportedType)
	}
}

func TestCreate_InsideManagedDirectory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	parentDir := filepath.Join(home, ".config", "awesome")
	testhelpers.MakeDir(t, parentDir)
	if err := svc.Add(context.Background(), "", []string{parentDir}); err != nil {
		t.Fatalf("Add parent dir: %v", err)
	}

	childPath := filepath.Join(home, ".config", "awesome", "child.conf")
	err := svc.Create(context.Background(), "", []string{childPath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error creating inside managed directory, got nil")
	}
	if !errors.Is(err, lnkerror.ErrAlreadyManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrAlreadyManaged)
	}
}

func TestCreate_AlreadyManaged(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")
	if err := svc.Add(context.Background(), "", []string{filePath}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error creating already-managed path, got nil")
	}
	if !errors.Is(err, lnkerror.ErrAlreadyManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrAlreadyManaged)
	}
}

func TestCreate_PathOutsideHome(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	outsidePath := filepath.Join(os.TempDir(), "lnk-test-outside-create")
	defer os.Remove(outsidePath)

	err := svc.Create(context.Background(), "", []string{outsidePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error for path outside $HOME, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNotInHome) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotInHome)
	}
}

func TestCreate_InsideGitRepo(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	projectRoot := filepath.Join(home, "project")
	testhelpers.MakeDir(t, projectRoot)
	testhelpers.InitGitRepo(t, projectRoot)

	filePath := filepath.Join(projectRoot, "config")
	err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error creating inside a git repo, got nil")
	}
	if !errors.Is(err, lnkerror.ErrInsideGitRepo) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrInsideGitRepo)
	}
}

func TestCreate_NotInitialized(t *testing.T) {
	svc, repoPath := testhelpers.NewTestRepo(t)
	_ = repoPath

	filePath := filepath.Join("/nonexistent", "home", ".bashrc")
	err := svc.Create(context.Background(), "", []string{filePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error when repo not initialized, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNotInitialized) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotInitialized)
	}
}

func TestCreate_DuplicateInSameCall(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	err := svc.Create(context.Background(), "", []string{filePath, filePath}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error for duplicate path in same call, got nil")
	}
	if !errors.Is(err, lnkerror.ErrDuplicatePath) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrDuplicatePath)
	}
}

func TestCreate_EmptyPaths(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	err := svc.Create(context.Background(), "", []string{}, service.CreateOptions{})
	if err == nil {
		t.Fatal("expected error for empty paths, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNoPaths) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNoPaths)
	}
}
