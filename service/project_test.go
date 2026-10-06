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
	"go.fuzzyporpoise.dev/lnk/v2/internal/resolver"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

func initProjectRepo(t *testing.T, dir string) {
	t.Helper()
	testhelpers.InitGitRepo(t, dir)
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:User/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	// Create a file and commit so HEAD exists.
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# repo\n"), 0o644); err != nil {
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

func TestProjectInit_CreatesLnkInclude(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	created, err := ps.ProjectInit(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectInit: %v", err)
	}
	if !created {
		t.Error("expected created=true")
	}

	manifest := filepath.Join(repoDir, ".lnkinclude")
	if !testhelpers.FileExists(t, manifest) {
		t.Error("expected .lnkinclude to be created")
	}
}

func TestProjectInit_Idempotent(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, err := ps.ProjectInit(context.Background(), repoDir); err != nil {
		t.Fatalf("ProjectInit: %v", err)
	}

	created, err := ps.ProjectInit(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectInit second call: %v", err)
	}
	if created {
		t.Error("expected created=false on second call")
	}
}

func TestProjectInit_RequiresGitRepo(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	notARepo := filepath.Join(home, "not-a-repo")
	testhelpers.MakeDir(t, notARepo)

	ps := service.NewProjectService(svc)
	_, err := ps.ProjectInit(context.Background(), notARepo)
	if err == nil {
		t.Fatal("expected error outside git repo")
	}
	if !errors.Is(err, lnkerror.ErrOutsideGitRepo) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrOutsideGitRepo)
	}
}

func TestProjectInit_FromSubdirAnchorsAtGitRoot(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	subDir := filepath.Join(repoDir, "sub", "dir")
	testhelpers.MakeDir(t, subDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	created, err := ps.ProjectInit(context.Background(), subDir)
	if err != nil {
		t.Fatalf("ProjectInit: %v", err)
	}
	if !created {
		t.Error("expected created=true")
	}

	if !testhelpers.FileExists(t, filepath.Join(repoDir, ".lnkinclude")) {
		t.Error("expected .lnkinclude at the git root")
	}
	if testhelpers.FileExists(t, filepath.Join(subDir, ".lnkinclude")) {
		t.Error("expected no .lnkinclude in the subdirectory")
	}
}

func TestProjectCommands_RefuseLnkRepository(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	cloneDir := filepath.Join(home, "elsewhere", "dotfiles-clone")

	// A copy of the lnk repo at a different path still carries the marker.
	testhelpers.MakeDir(t, cloneDir)
	testhelpers.InitGitRepo(t, cloneDir)
	if err := os.WriteFile(filepath.Join(cloneDir, ".lnkrepo"), []byte("version=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{svc.RepoPath(), cloneDir} {
		ps := service.NewProjectService(svc)
		if _, err := ps.ProjectInit(context.Background(), dir); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectInit(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, err := ps.ProjectPush(context.Background(), dir, false); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectPush(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, _, err := ps.ProjectListPatterns(context.Background(), dir); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectListPatterns(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, err := ps.ProjectUntrackPattern(context.Background(), dir, "x", true); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectUntrackPattern(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, err := ps.ProjectRestore(context.Background(), dir, false, false); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectRestore(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, err := ps.ProjectSync(context.Background(), dir, false, false, false); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectSync(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, err := ps.ProjectRemove(context.Background(), dir); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectRemove(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
		if _, err := ps.ProjectForget(context.Background(), dir); !errors.Is(err, lnkerror.ErrIsLnkRepository) {
			t.Errorf("ProjectForget(%s) error = %v, want %v", dir, err, lnkerror.ErrIsLnkRepository)
		}
	}
}

func TestProjectAddPattern_AppendsAndNormalizes(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	ps := service.NewProjectService(svc)
	normalized, matched, err := ps.ProjectAddPattern(context.Background(), repoDir, liveFile)
	if err != nil {
		t.Fatalf("ProjectAddPattern: %v", err)
	}
	if normalized != filepath.Join(".cursor", "rules.md") {
		t.Errorf("normalized = %q, want .cursor/rules.md", normalized)
	}
	if !matched {
		t.Error("expected matched=true for an existing file")
	}

	manifest := filepath.Join(repoDir, ".lnkinclude")
	content, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != filepath.Join(".cursor", "rules.md")+"\n" {
		t.Errorf(".lnkinclude content = %q", string(content))
	}
}

func TestProjectAddPattern_StoresPatternsVerbatim(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)

	// A ! negation is not a path and must be stored as written.
	negation, matched, err := ps.ProjectAddPattern(context.Background(), repoDir, "!AGENTS.md")
	if err != nil {
		t.Fatalf("add negation: %v", err)
	}
	if negation != "!AGENTS.md" {
		t.Errorf("negation = %q, want !AGENTS.md", negation)
	}
	if !matched {
		t.Error("expected matched=true for negations (match check is skipped)")
	}

	// A glob for a directory that does not exist yet keeps its trailing slash.
	glob, matched, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/")
	if err != nil {
		t.Fatalf("add glob: %v", err)
	}
	if glob != ".todo/" {
		t.Errorf("glob = %q, want .todo/", glob)
	}
	if matched {
		t.Error("expected matched=false for a pattern with no current files")
	}

	manifest := filepath.Join(repoDir, ".lnkinclude")
	content, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "!AGENTS.md\n.todo/\n" {
		t.Errorf(".lnkinclude content = %q", string(content))
	}
}

func TestProjectAddPattern_RelativizesExistingDir(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	testhelpers.MakeFile(t, filepath.Join(repoDir, ".todo", "a.md"), "a\n")

	ps := service.NewProjectService(svc)
	normalized, matched, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if normalized != ".todo" {
		t.Errorf("normalized = %q, want .todo", normalized)
	}
	if !matched {
		t.Error("expected matched=true for a dir with files")
	}
}

func TestProjectAddPattern_NegatesManagedSymlink(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "AGENTS.md"); err != nil {
		t.Fatalf("add include: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, "AGENTS.md"), "agents\n")
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	// AGENTS.md is now a symlink into lnk storage (outside the project).
	// Negating it must still relativize to its live path.
	negation, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "!AGENTS.md")
	if err != nil {
		t.Fatalf("add negation: %v", err)
	}
	if negation != "!AGENTS.md" {
		t.Errorf("negation = %q, want !AGENTS.md", negation)
	}
}

func TestProjectAddPattern_DotDotFilenameInsideProject(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	testhelpers.MakeFile(t, filepath.Join(repoDir, "..foo"), "odd name\n")

	ps := service.NewProjectService(svc)
	normalized, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "..foo")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if normalized != "..foo" {
		t.Errorf("normalized = %q, want ..foo", normalized)
	}
}

func TestProjectAddPattern_RejectsOutsideProject(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	outside := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, outside, "# bashrc\n")

	ps := service.NewProjectService(svc)
	_, _, err := ps.ProjectAddPattern(context.Background(), repoDir, outside)
	if err == nil {
		t.Fatal("expected error for pattern outside project")
	}
	if !errors.Is(err, lnkerror.ErrOutsideProject) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrOutsideProject)
	}
}

func TestProjectAddPattern_RejectsEmpty(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	for _, pattern := range []string{"", "   ", "!"} {
		if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, pattern); !errors.Is(err, lnkerror.ErrEmptyPattern) {
			t.Errorf("add %q error = %v, want %v", pattern, err, lnkerror.ErrEmptyPattern)
		}
	}
}

func TestProjectAddPattern_RejectsDuplicate(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	pattern := ".cursor/**"
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, pattern); err != nil {
		t.Fatalf("first add: %v", err)
	}

	_, _, err := ps.ProjectAddPattern(context.Background(), repoDir, pattern)
	if err == nil {
		t.Fatal("expected error for duplicate pattern")
	}
	if !errors.Is(err, lnkerror.ErrAlreadyManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrAlreadyManaged)
	}
}

func TestProjectListPatterns_SplitsGlobalAndLocal(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	globalPath := filepath.Join(svc.RepoPath(), ".lnkinclude")
	if err := os.WriteFile(globalPath, []byte(".todo/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add: %v", err)
	}

	global, local, err := ps.ProjectListPatterns(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectListPatterns: %v", err)
	}
	if len(global) != 1 || global[0] != ".todo/**" {
		t.Errorf("global = %v, want [.todo/**]", global)
	}
	if len(local) != 1 || local[0] != ".cursor/**" {
		t.Errorf("local = %v, want [.cursor/**]", local)
	}
}

func TestProjectListPatterns_FromSubdirSeesRootManifest(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	subDir := filepath.Join(repoDir, "sub")
	testhelpers.MakeDir(t, subDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add: %v", err)
	}

	_, local, err := ps.ProjectListPatterns(context.Background(), subDir)
	if err != nil {
		t.Fatalf("ProjectListPatterns: %v", err)
	}
	if len(local) != 1 || local[0] != ".cursor/**" {
		t.Errorf("local = %v, want [.cursor/**]", local)
	}
}

func TestProjectUntrackPattern_RemovesLocal(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "agents.md"); err != nil {
		t.Fatalf("add: %v", err)
	}

	result, err := ps.ProjectUntrackPattern(context.Background(), repoDir, "agents.md", true)
	if err != nil {
		t.Fatalf("ProjectUntrackPattern: %v", err)
	}
	if !result.Removed {
		t.Error("expected removed=true")
	}
	if result.IsGlobal {
		t.Error("expected isGlobal=false")
	}
}

func TestProjectUntrackPattern_WarnsForGlobal(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	globalPath := filepath.Join(svc.RepoPath(), ".lnkinclude")
	if err := os.WriteFile(globalPath, []byte(".todo/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := service.NewProjectService(svc)
	result, err := ps.ProjectUntrackPattern(context.Background(), repoDir, ".todo/**", true)
	if err != nil {
		t.Fatalf("ProjectUntrackPattern: %v", err)
	}
	if result.Removed {
		t.Error("expected removed=false")
	}
	if !result.IsGlobal {
		t.Error("expected isGlobal=true")
	}
}

func TestProjectUntrackPattern_ErrorsWhenMissing(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	_, err := ps.ProjectUntrackPattern(context.Background(), repoDir, "missing.md", true)
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
	if !errors.Is(err, lnkerror.ErrNotManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotManaged)
	}
}

func TestProjectPush_MovesMatchingFileToStorage(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0] != ".cursor/rules.md" {
		t.Errorf("synced = %v, want [.cursor/rules.md]", result.Synced)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".cursor", "rules.md")
	if !testhelpers.FileExists(t, storageFile) {
		t.Errorf("expected storage file %s", storageFile)
	}
	testhelpers.AssertSymlink(t, liveFile, storageFile)

	if logs := testhelpers.GitLog(t, svc.RepoPath()); len(logs) < 2 {
		t.Errorf("expected at least 2 commits, got %d", len(logs))
	}
}

func TestProjectPush_CommitScopedToProjects(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".cursor", "rules.md"), "# rules\n")

	// An unrelated, uncommitted change elsewhere in the lnk repo. A project
	// command must not sweep it into the automatic commit.
	unrelated := filepath.Join(svc.RepoPath(), "common.lnk", "unrelated.txt")
	testhelpers.MakeFile(t, unrelated, "unrelated\n")

	before := len(testhelpers.GitLog(t, svc.RepoPath()))
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	after := testhelpers.GitLog(t, svc.RepoPath())
	if len(after) != before+1 {
		t.Fatalf("expected exactly 1 new commit, before=%d after=%d", before, len(after))
	}

	if out, err := exec.Command("git", "-C", svc.RepoPath(), "ls-files", "--error-unmatch", "common.lnk/unrelated.txt").CombinedOutput(); err == nil {
		t.Errorf("unrelated change was committed: %s", out)
	}
	if _, err := exec.Command("git", "-C", svc.RepoPath(), "ls-files", "--error-unmatch", "projects").CombinedOutput(); err != nil {
		t.Errorf("project storage should be committed: %v", err)
	}
	if got, want := gitAuthor(t, svc.RepoPath()), "Lnk User <lnk@localhost>"; got != want {
		t.Errorf("author = %q, want %q", got, want)
	}
}

func TestProjectPush_DoesNotCommitStagedUnrelatedChanges(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".cursor", "rules.md"), "# rules\n")

	// An unrelated change that is already staged, as a crashed earlier lnk
	// command would leave it. The project commit must leave it staged and
	// uncommitted.
	unrelated := filepath.Join(svc.RepoPath(), "common.lnk", "unrelated.txt")
	testhelpers.MakeFile(t, unrelated, "unrelated\n")
	if out, err := exec.Command("git", "-C", svc.RepoPath(), "add", "common.lnk/unrelated.txt").CombinedOutput(); err != nil {
		t.Fatalf("stage unrelated: %v\n%s", err, out)
	}

	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	if out, err := exec.Command("git", "-C", svc.RepoPath(), "cat-file", "-e", "HEAD:common.lnk/unrelated.txt").CombinedOutput(); err == nil {
		t.Errorf("staged unrelated change was swept into the project commit: %s", out)
	}
	out, err := exec.Command("git", "-C", svc.RepoPath(), "status", "--porcelain", "--", "common.lnk/unrelated.txt").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "A  common.lnk/unrelated.txt"; got != want {
		t.Errorf("unrelated change status = %q, want %q (still staged)", got, want)
	}
}

func TestProjectSync_NoCommitWhenOnlyUnrelatedDirty(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".cursor", "rules.md"), "# rules\n")
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	// Dirty the tree outside projects/ with no project-side changes.
	testhelpers.MakeFile(t, filepath.Join(svc.RepoPath(), "common.lnk", "unrelated.txt"), "unrelated\n")

	before := len(testhelpers.GitLog(t, svc.RepoPath()))
	if _, err := ps.ProjectSync(context.Background(), repoDir, false, false, false); err != nil {
		t.Fatalf("ProjectSync: %v", err)
	}
	after := testhelpers.GitLog(t, svc.RepoPath())
	if len(after) != before {
		t.Errorf("expected no new commit for unrelated dirty tree, before=%d after=%d", before, len(after))
	}
}

func TestProjectPush_FromSubdirStoresRootRelative(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	subDir := filepath.Join(repoDir, "sub")
	testhelpers.MakeDir(t, subDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "notes.md"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, "notes.md"), "notes\n")
	testhelpers.MakeFile(t, filepath.Join(subDir, "notes.md"), "sub notes\n")

	result, err := ps.ProjectPush(context.Background(), subDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if len(result.Synced) != 2 {
		t.Fatalf("synced = %v, want 2 entries", result.Synced)
	}

	id, err := resolver.ResolveProjectID(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("resolve project id: %v", err)
	}
	for _, rel := range []string{"notes.md", filepath.Join("sub", "notes.md")} {
		storageFile := filepath.Join(svc.RepoPath(), "projects", id, rel)
		if !testhelpers.FileExists(t, storageFile) {
			t.Errorf("expected storage file %s", storageFile)
		}
	}
}

func TestProjectPush_SkipsAlreadySymlinked(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("first push: %v", err)
	}

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if len(result.Synced) != 0 {
		t.Errorf("synced = %v, want empty", result.Synced)
	}
}

func TestProjectPush_ErrorsWhenNoPatterns(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	_, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err == nil {
		t.Fatal("expected error when no patterns")
	}
	if !errors.Is(err, lnkerror.ErrNoPatterns) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNoPatterns)
	}
}

func TestProjectPush_SkipsGitDirectory(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "*.md"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	gitConfig := filepath.Join(repoDir, ".git", "config")
	if _, err := os.Stat(gitConfig); err != nil {
		t.Fatalf("expected .git/config to exist: %v", err)
	}

	liveFile := filepath.Join(repoDir, "README.md")
	testhelpers.MakeFile(t, liveFile, "# hello\n")

	// Force: README.md is committed to the project repo by the fixture.
	result, err := ps.ProjectPush(context.Background(), repoDir, true)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0] != "README.md" {
		t.Errorf("synced = %v, want [README.md]", result.Synced)
	}

	if !testhelpers.FileExists(t, gitConfig) {
		t.Error("expected .git/config to remain in place")
	}
}

func TestProjectPush_SkipsProjectGitTrackedFiles(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "*.md"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	trackedFile := filepath.Join(repoDir, "README.md") // committed by fixture
	untrackedFile := filepath.Join(repoDir, "notes.md")
	testhelpers.MakeFile(t, untrackedFile, "notes\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0] != "notes.md" {
		t.Errorf("synced = %v, want [notes.md]", result.Synced)
	}
	if len(result.SkippedTracked) != 1 || result.SkippedTracked[0] != "README.md" {
		t.Errorf("skipped = %v, want [README.md]", result.SkippedTracked)
	}

	// The tracked file must remain a real file, not a symlink.
	info, err := os.Lstat(trackedFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected tracked file to remain a real file")
	}

	// Force manages it anyway.
	forced, err := ps.ProjectPush(context.Background(), repoDir, true)
	if err != nil {
		t.Fatalf("ProjectPush --force: %v", err)
	}
	if len(forced.Synced) != 1 || forced.Synced[0] != "README.md" {
		t.Errorf("forced synced = %v, want [README.md]", forced.Synced)
	}
	storageFile := filepath.Join(svc.RepoPath(), "projects", forced.ProjectID, "README.md")
	testhelpers.AssertSymlink(t, trackedFile, storageFile)
}

func TestProjectPush_PrunesNestedGitRepos(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	nested := filepath.Join(repoDir, "vendor", "lib")
	testhelpers.MakeDir(t, nested)
	testhelpers.InitGitRepo(t, nested)
	nestedFile := filepath.Join(nested, "secret.md")
	testhelpers.MakeFile(t, nestedFile, "nested\n")

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "vendor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if len(result.Synced) != 0 {
		t.Errorf("synced = %v, want empty (nested repo pruned)", result.Synced)
	}

	info, err := os.Lstat(nestedFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected nested repo file to remain a real file")
	}
}

func TestProjectPush_NeverStoresLnkMetadata(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "*"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	testhelpers.MakeFile(t, filepath.Join(repoDir, "notes.md"), "notes\n")
	testhelpers.MakeFile(t, filepath.Join(repoDir, "old.md.lnk-backup"), "backup\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0] != "notes.md" {
		t.Errorf("synced = %v, want [notes.md]", result.Synced)
	}

	storageDir := filepath.Join(svc.RepoPath(), "projects", result.ProjectID)
	if testhelpers.FileExists(t, filepath.Join(storageDir, ".lnkinclude")) {
		t.Error("expected .lnkinclude to never be stored")
	}
	if testhelpers.FileExists(t, filepath.Join(storageDir, "old.md.lnk-backup")) {
		t.Error("expected .lnk-backup files to never be stored")
	}
	if !testhelpers.FileExists(t, filepath.Join(repoDir, ".lnkinclude")) {
		t.Error("expected live .lnkinclude to remain in place")
	}
}

func TestProjectPush_ReportsMoveFailures(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "blocked.md"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, "blocked.md"), "blocked\n")

	// A directory at the storage path makes the move fail.
	id, err := resolver.ResolveProjectID(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("resolve project id: %v", err)
	}
	testhelpers.MakeDir(t, filepath.Join(svc.RepoPath(), "projects", id, "blocked.md"))

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err == nil {
		t.Fatal("expected error when the move fails")
	}
	if !errors.Is(err, lnkerror.ErrSyncFailed) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrSyncFailed)
	}
	if len(result.Synced) != 0 {
		t.Errorf("synced = %v, want empty", result.Synced)
	}

	// The live file must not be lost.
	if !testhelpers.FileExists(t, filepath.Join(repoDir, "blocked.md")) {
		t.Error("expected live file to remain after failed sync")
	}
}

func TestProjectPush_NoOriginUsesLocalID(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "localonly")
	testhelpers.MakeDir(t, repoDir)
	testhelpers.InitGitRepo(t, repoDir) // no origin remote

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, "notes.md"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, "notes.md"), "notes\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	if !strings.HasPrefix(result.ProjectID, "local/") {
		t.Errorf("project id = %q, want local/ prefix", result.ProjectID)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, "notes.md")
	if !testhelpers.FileExists(t, storageFile) {
		t.Errorf("expected storage file %s", storageFile)
	}
}

func TestProjectRestore_RecreatesSymlinks(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".cursor", "rules.md")
	if err := os.Remove(liveFile); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}

	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 1 {
		t.Errorf("restored = %v, want 1 entry", info.Restored)
	}
	testhelpers.AssertSymlink(t, liveFile, storageFile)
}

func TestProjectRestore_BackupsExistingFile(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".cursor", "rules.md")
	if err := os.Remove(liveFile); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	testhelpers.MakeFile(t, liveFile, "local changes\n")

	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 1 || len(info.BackedUp) != 1 {
		t.Errorf("restored = %v, backed up = %v", info.Restored, info.BackedUp)
	}

	testhelpers.AssertSymlink(t, liveFile, storageFile)
	backupFile := liveFile + ".lnk-backup"
	if !testhelpers.FileExists(t, backupFile) {
		t.Error("expected .lnk-backup file")
	}
}

func TestProjectRestore_SkipsProjectGitTrackedFiles(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	// Simulate a fresh clone: the live path is a real file tracked upstream.
	if err := os.Remove(liveFile); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	testhelpers.MakeFile(t, liveFile, "upstream content\n")
	if out, err := exec.Command("git", "-C", repoDir, "add", ".cursor/rules.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 0 {
		t.Errorf("restored = %v, want empty", info.Restored)
	}
	if len(info.SkippedTracked) != 1 || info.SkippedTracked[0] != ".cursor/rules.md" {
		t.Errorf("skipped = %v, want [.cursor/rules.md]", info.SkippedTracked)
	}
	if len(info.BackedUp) != 0 {
		t.Errorf("backed up = %v, want empty", info.BackedUp)
	}

	liveInfo, err := os.Lstat(liveFile)
	if err != nil {
		t.Fatal(err)
	}
	if liveInfo.Mode()&os.ModeSymlink != 0 {
		t.Error("expected tracked live file to remain a real file")
	}

	// Force replaces it with a symlink, backing the real file up.
	forced, err := ps.ProjectRestore(context.Background(), repoDir, false, true)
	if err != nil {
		t.Fatalf("ProjectRestore --force: %v", err)
	}
	if len(forced.Restored) != 1 || len(forced.BackedUp) != 1 {
		t.Errorf("forced restored = %v, backed up = %v", forced.Restored, forced.BackedUp)
	}
	liveInfo, err = os.Lstat(liveFile)
	if err != nil {
		t.Fatal(err)
	}
	if liveInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink after forced restore")
	}
}

func TestProjectRestore_DryRun(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	if err := os.Remove(liveFile); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}

	info, err := ps.ProjectRestore(context.Background(), repoDir, true, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 1 {
		t.Errorf("restored = %v, want 1 entry", info.Restored)
	}

	if _, err := os.Lstat(liveFile); err == nil {
		t.Error("expected symlink not to be created in dry-run mode")
	}
}

func TestProjectPull_PullsAndRestores(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	remote := testhelpers.NewBareRemote(t)
	cmds := [][]string{
		{"git", "-C", svc.RepoPath(), "remote", "add", "origin", remote},
		{"git", "-C", svc.RepoPath(), "push", "-u", "origin", "main"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".cursor", "rules.md")
	if err := os.Remove(liveFile); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}

	info, err := ps.ProjectPull(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPull: %v", err)
	}
	if len(info.Restored) != 1 {
		t.Errorf("restored = %v, want 1 entry", info.Restored)
	}
	testhelpers.AssertSymlink(t, liveFile, storageFile)
}

// ---------- Lifecycle and reconciliation ----------

// newPushedProject creates a project repo with an origin remote, adds the
// pattern, creates the given files (slash-separated, root-relative), and
// pushes. It returns the lnk service, project service, repo dir, and
// project id.
func newPushedProject(t *testing.T, pattern string, files ...string) (*service.Service, *service.ProjectService, string, string) {
	t.Helper()
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, pattern); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	for _, rel := range files {
		testhelpers.MakeFile(t, filepath.Join(repoDir, filepath.FromSlash(rel)), "content of "+rel+"\n")
	}
	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	return svc, ps, repoDir, result.ProjectID
}

func assertRealFile(t *testing.T, path, wantContent string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("expected real file at %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected %s to be a real file, got symlink", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != wantContent {
		t.Errorf("content of %s = %q, want %q", path, data, wantContent)
	}
}

func TestProjectUntrackPattern_ReleasesNowUnmatchedFiles(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".todo/**", ".todo/a.md", ".todo/b.md")
	storageDir := filepath.Join(svc.RepoPath(), "projects", id)

	result, err := ps.ProjectUntrackPattern(context.Background(), repoDir, ".todo/**", false)
	if err != nil {
		t.Fatalf("ProjectUntrackPattern: %v", err)
	}
	if !result.Removed {
		t.Error("expected removed=true")
	}
	if len(result.Released) != 2 {
		t.Errorf("released = %v, want 2 entries", result.Released)
	}

	assertRealFile(t, filepath.Join(repoDir, ".todo", "a.md"), "content of .todo/a.md\n")
	assertRealFile(t, filepath.Join(repoDir, ".todo", "b.md"), "content of .todo/b.md\n")
	if testhelpers.FileExists(t, filepath.Join(storageDir, ".todo")) {
		t.Error("expected .todo storage to be pruned")
	}
}

func TestProjectUntrackPattern_KeepLeavesFilesManaged(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".todo/**", ".todo/a.md")
	storageFile := filepath.Join(svc.RepoPath(), "projects", id, ".todo", "a.md")
	liveFile := filepath.Join(repoDir, ".todo", "a.md")

	result, err := ps.ProjectUntrackPattern(context.Background(), repoDir, ".todo/**", true)
	if err != nil {
		t.Fatalf("ProjectUntrackPattern: %v", err)
	}
	if !result.Removed {
		t.Error("expected removed=true")
	}
	if len(result.Released) != 0 {
		t.Errorf("released = %v, want empty with keep", result.Released)
	}
	testhelpers.AssertSymlink(t, liveFile, storageFile)
	if !testhelpers.FileExists(t, storageFile) {
		t.Error("expected storage copy to remain")
	}
}

func TestProjectUntrackPattern_KeepsStillMatchedFiles(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	for _, pattern := range []string{"*.md", "notes.md"} {
		if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, pattern); err != nil {
			t.Fatalf("add %s: %v", pattern, err)
		}
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, "notes.md"), "notes\n")
	testhelpers.MakeFile(t, filepath.Join(repoDir, "other.md"), "other\n")
	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}
	storageDir := filepath.Join(svc.RepoPath(), "projects", result.ProjectID)

	untrack, err := ps.ProjectUntrackPattern(context.Background(), repoDir, "*.md", false)
	if err != nil {
		t.Fatalf("ProjectUntrackPattern: %v", err)
	}
	if len(untrack.Released) != 1 || untrack.Released[0] != "other.md" {
		t.Errorf("released = %v, want [other.md]", untrack.Released)
	}

	// notes.md is still matched by its dedicated pattern.
	testhelpers.AssertSymlink(t, filepath.Join(repoDir, "notes.md"), filepath.Join(storageDir, "notes.md"))
	assertRealFile(t, filepath.Join(repoDir, "other.md"), "other\n")
}

func TestProjectSync_PatternDrift(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, "**/*.md", ".todo/a.md", "notes.md")
	storageDir := filepath.Join(svc.RepoPath(), "projects", id)

	// Simulate a hand-edited manifest that drops the .todo pattern.
	manifest := filepath.Join(repoDir, ".lnkinclude")
	if err := os.WriteFile(manifest, []byte("notes.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ps.ProjectSync(context.Background(), repoDir, false, false, false)
	if err != nil {
		t.Fatalf("ProjectSync: %v", err)
	}
	if len(result.Released) != 1 || result.Released[0] != ".todo/a.md" {
		t.Errorf("released = %v, want [.todo/a.md]", result.Released)
	}
	if len(result.Synced) != 0 {
		t.Errorf("synced = %v, want empty", result.Synced)
	}
	if len(result.Deletions) != 0 {
		t.Errorf("deletions = %v, want empty", result.Deletions)
	}

	assertRealFile(t, filepath.Join(repoDir, ".todo", "a.md"), "content of .todo/a.md\n")
	if testhelpers.FileExists(t, filepath.Join(storageDir, ".todo")) {
		t.Error("expected .todo storage to be pruned")
	}
	// The still-matched file remains managed.
	testhelpers.AssertSymlink(t, filepath.Join(repoDir, "notes.md"), filepath.Join(storageDir, "notes.md"))
}

func TestProjectSync_NewMatches(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".todo/**", ".todo/a.md")

	newFile := filepath.Join(repoDir, ".todo", "b.md")
	testhelpers.MakeFile(t, newFile, "b\n")

	result, err := ps.ProjectSync(context.Background(), repoDir, false, false, false)
	if err != nil {
		t.Fatalf("ProjectSync: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0] != ".todo/b.md" {
		t.Errorf("synced = %v, want [.todo/b.md]", result.Synced)
	}
	testhelpers.AssertSymlink(t, newFile, filepath.Join(svc.RepoPath(), "projects", id, ".todo", "b.md"))
}

func TestProjectSync_LiveDeletionsReportedThenPruned(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".todo/**", ".todo/a.md", ".todo/b.md")
	storageFile := filepath.Join(svc.RepoPath(), "projects", id, ".todo", "a.md")

	// The user deletes the live symlink.
	if err := os.Remove(filepath.Join(repoDir, ".todo", "a.md")); err != nil {
		t.Fatal(err)
	}

	result, err := ps.ProjectSync(context.Background(), repoDir, false, false, false)
	if err != nil {
		t.Fatalf("ProjectSync: %v", err)
	}
	if len(result.Deletions) != 1 || result.Deletions[0] != ".todo/a.md" {
		t.Errorf("deletions = %v, want [.todo/a.md]", result.Deletions)
	}
	if len(result.Pruned) != 0 {
		t.Errorf("pruned = %v, want empty without --prune-deletions", result.Pruned)
	}
	if !testhelpers.FileExists(t, storageFile) {
		t.Error("expected storage copy to be kept")
	}

	pruned, err := ps.ProjectSync(context.Background(), repoDir, false, true, false)
	if err != nil {
		t.Fatalf("ProjectSync --prune-deletions: %v", err)
	}
	if len(pruned.Pruned) != 1 || pruned.Pruned[0] != ".todo/a.md" {
		t.Errorf("pruned = %v, want [.todo/a.md]", pruned.Pruned)
	}
	if testhelpers.FileExists(t, storageFile) {
		t.Error("expected storage copy to be pruned")
	}
}

func TestProjectSync_DryRun(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".todo/**", ".todo/a.md")
	storageFile := filepath.Join(svc.RepoPath(), "projects", id, ".todo", "a.md")
	liveFile := filepath.Join(repoDir, ".todo", "a.md")

	manifest := filepath.Join(repoDir, ".lnkinclude")
	if err := os.WriteFile(manifest, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	before := len(testhelpers.GitLog(t, svc.RepoPath()))

	result, err := ps.ProjectSync(context.Background(), repoDir, true, false, false)
	if err != nil {
		t.Fatalf("ProjectSync --dry-run: %v", err)
	}
	if len(result.Released) != 1 || result.Released[0] != ".todo/a.md" {
		t.Errorf("released = %v, want [.todo/a.md]", result.Released)
	}

	testhelpers.AssertSymlink(t, liveFile, storageFile)
	if !testhelpers.FileExists(t, storageFile) {
		t.Error("expected storage copy untouched in dry-run")
	}
	if after := len(testhelpers.GitLog(t, svc.RepoPath())); after != before {
		t.Errorf("expected no commit in dry-run, log grew from %d to %d", before, after)
	}
}

func TestProjectSync_SkipsProjectGitTrackedUnlessForced(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	testhelpers.MakeFile(t, filepath.Join(repoDir, "tracked.md"), "tracked\n")
	if out, err := exec.Command("git", "-C", repoDir, "add", "tracked.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".lnkinclude"), []byte("*.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := service.NewProjectService(svc)
	result, err := ps.ProjectSync(context.Background(), repoDir, false, false, false)
	if err != nil {
		t.Fatalf("ProjectSync: %v", err)
	}
	if len(result.Synced) != 0 {
		t.Errorf("synced = %v, want empty", result.Synced)
	}
	// Both the fixture README.md and tracked.md are git-tracked.
	if len(result.SkippedTracked) != 2 {
		t.Errorf("skipped = %v, want 2 entries", result.SkippedTracked)
	}

	forced, err := ps.ProjectSync(context.Background(), repoDir, false, false, true)
	if err != nil {
		t.Fatalf("ProjectSync --force: %v", err)
	}
	if len(forced.Synced) != 2 {
		t.Errorf("forced synced = %v, want 2 entries", forced.Synced)
	}
}

func TestProjectSyncAll_DiscoversAndSyncsMultipleProjects(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	parent := filepath.Join(home, "repos")

	// First project: alpha.
	alphaDir := filepath.Join(parent, "alpha")
	testhelpers.MakeDir(t, alphaDir)
	initProjectRepoWithRemote(t, alphaDir, "git@github.com:User/Alpha.git")
	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), alphaDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern alpha: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(alphaDir, ".todo", "a.md"), "alpha\n")
	if _, err := ps.ProjectPush(context.Background(), alphaDir, false); err != nil {
		t.Fatalf("push alpha: %v", err)
	}

	// Second project: beta.
	betaDir := filepath.Join(parent, "beta")
	testhelpers.MakeDir(t, betaDir)
	initProjectRepoWithRemote(t, betaDir, "git@github.com:User/Beta.git")
	if _, _, err := ps.ProjectAddPattern(context.Background(), betaDir, "notes.md"); err != nil {
		t.Fatalf("add pattern beta: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(betaDir, "notes.md"), "beta\n")
	if _, err := ps.ProjectPush(context.Background(), betaDir, false); err != nil {
		t.Fatalf("push beta: %v", err)
	}

	// Add new files and run sync --all over the parent directory.
	testhelpers.MakeFile(t, filepath.Join(alphaDir, ".todo", "b.md"), "alpha b\n")
	testhelpers.MakeFile(t, filepath.Join(betaDir, "notes2.md"), "beta 2\n")
	if _, _, err := ps.ProjectAddPattern(context.Background(), betaDir, "notes2.md"); err != nil {
		t.Fatalf("add pattern notes2: %v", err)
	}

	// Populate the local cache by discovering checkouts under parent.
	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{parent}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}

	result, err := ps.ProjectSyncAll(context.Background(), false, false, false)
	if err != nil {
		t.Fatalf("ProjectSyncAll: %v", err)
	}
	if len(result.Unavailable) != 0 {
		t.Errorf("unavailable = %v, want none", result.Unavailable)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(result.Results))
	}

	byID := map[string]int{}
	for _, r := range result.Results {
		byID[r.ProjectID] = len(r.Synced)
	}
	if byID["github.com/user/alpha"] != 1 {
		t.Errorf("alpha synced = %d, want 1", byID["github.com/user/alpha"])
	}
	if byID["github.com/user/beta"] != 1 {
		t.Errorf("beta synced = %d, want 1", byID["github.com/user/beta"])
	}
}

func TestProjectSyncAll_ReportsUnavailable(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	parent := filepath.Join(home, "repos")

	// Push a project but do not discover its checkout.
	repoDir := filepath.Join(parent, "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)
	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".todo", "a.md"), "a\n")
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	// ProjectPush records the cache; clear it and then discover an empty
	// directory so the cache records the project as missing.
	if err := os.Remove(service.RegistryPath()); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clear cache: %v", err)
	}
	emptyDir := filepath.Join(home, "empty")
	testhelpers.MakeDir(t, emptyDir)
	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{emptyDir}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}

	result, err := ps.ProjectSyncAll(context.Background(), false, false, false)
	if err != nil {
		t.Fatalf("ProjectSyncAll: %v", err)
	}
	if len(result.Results) != 0 {
		t.Errorf("results = %v, want none", result.Results)
	}
	if len(result.Unavailable) != 1 || result.Unavailable[0] != "github.com/user/repo" {
		t.Errorf("unavailable = %v, want [github.com/user/repo]", result.Unavailable)
	}
}

func TestProjectSyncAll_DryRun(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".todo/**", ".todo/a.md")
	parent := filepath.Dir(repoDir)

	newFile := filepath.Join(repoDir, ".todo", "b.md")
	testhelpers.MakeFile(t, newFile, "b\n")

	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{parent}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}

	before := len(testhelpers.GitLog(t, svc.RepoPath()))
	result, err := ps.ProjectSyncAll(context.Background(), true, false, false)
	if err != nil {
		t.Fatalf("ProjectSyncAll: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(result.Results))
	}
	if len(result.Results[0].Synced) != 1 || result.Results[0].Synced[0] != ".todo/b.md" {
		t.Errorf("synced = %v, want [.todo/b.md]", result.Results[0].Synced)
	}

	if testhelpers.FileExists(t, filepath.Join(svc.RepoPath(), "projects", id, ".todo", "b.md")) {
		t.Error("expected dry-run to leave storage untouched")
	}
	if after := len(testhelpers.GitLog(t, svc.RepoPath())); after != before {
		t.Errorf("expected no commit in dry-run, log grew from %d to %d", before, after)
	}
}

func TestProjectSyncAll_NoStoredProjects(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	scanDir := filepath.Join(home, "scan")
	testhelpers.MakeDir(t, scanDir)
	ps := service.NewProjectService(svc)

	result, err := ps.ProjectSyncAll(context.Background(), false, false, false)
	if err != nil {
		t.Fatalf("ProjectSyncAll: %v", err)
	}
	if len(result.Results) != 0 || len(result.Unavailable) != 0 {
		t.Errorf("unexpected results: %+v", result)
	}
}

func initProjectRepoWithRemote(t *testing.T, dir, remote string) {
	t.Helper()
	testhelpers.InitGitRepo(t, dir)
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", remote).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# repo\n"), 0o644); err != nil {
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

func TestProjectRemove_RestoresFilesAndDropsStorage(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".cursor/**", ".cursor/rules.md", ".cursor/extra.md")
	storageDir := filepath.Join(svc.RepoPath(), "projects", id)

	result, err := ps.ProjectRemove(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectRemove: %v", err)
	}
	if len(result.Restored) != 2 {
		t.Errorf("restored = %v, want 2 entries", result.Restored)
	}

	assertRealFile(t, filepath.Join(repoDir, ".cursor", "rules.md"), "content of .cursor/rules.md\n")
	assertRealFile(t, filepath.Join(repoDir, ".cursor", "extra.md"), "content of .cursor/extra.md\n")
	if testhelpers.FileExists(t, storageDir) {
		t.Error("expected project storage to be deleted")
	}

	// .lnkinclude stays for later re-adoption.
	if !testhelpers.FileExists(t, filepath.Join(repoDir, ".lnkinclude")) {
		t.Error("expected .lnkinclude to be left in place")
	}

	logs := testhelpers.GitLog(t, svc.RepoPath())
	found := false
	for _, msg := range logs {
		if strings.Contains(msg, "lnk: removed project") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a removal commit, got %v", logs)
	}
}

func TestProjectRemove_BacksUpConflictingLiveFile(t *testing.T) {
	_, ps, repoDir, _ := newPushedProject(t, ".cursor/**", ".cursor/rules.md")

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	if err := os.Remove(liveFile); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, liveFile, "local edits\n")

	result, err := ps.ProjectRemove(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectRemove: %v", err)
	}
	if len(result.BackedUp) != 1 || result.BackedUp[0] != ".cursor/rules.md" {
		t.Errorf("backed up = %v, want [.cursor/rules.md]", result.BackedUp)
	}

	assertRealFile(t, liveFile, "content of .cursor/rules.md\n")
	data, err := os.ReadFile(liveFile + ".lnk-backup")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "local edits\n" {
		t.Errorf("backup content = %q, want local edits", data)
	}
}

func TestProjectRemove_ErrorsWhenNotManaged(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	_, err := ps.ProjectRemove(context.Background(), repoDir)
	if !errors.Is(err, lnkerror.ErrNotManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotManaged)
	}
}

func TestProjectForget_RemovesSymlinksKeepsStorage(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".cursor/**", ".cursor/rules.md", ".cursor/extra.md")
	storageFile := filepath.Join(svc.RepoPath(), "projects", id, ".cursor", "rules.md")

	result, err := ps.ProjectForget(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectForget: %v", err)
	}
	if len(result.Unlinked) != 2 {
		t.Errorf("unlinked = %v, want 2 entries", result.Unlinked)
	}

	for _, rel := range []string{"rules.md", "extra.md"} {
		if _, err := os.Lstat(filepath.Join(repoDir, ".cursor", rel)); err == nil {
			t.Errorf("expected live path %s to be gone", rel)
		}
	}
	if !testhelpers.FileExists(t, storageFile) {
		t.Error("expected storage copy to be kept")
	}

	// The files can be brought back later.
	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore after forget: %v", err)
	}
	if len(info.Restored) != 2 {
		t.Errorf("restored = %v, want 2 entries", info.Restored)
	}
	testhelpers.AssertSymlink(t, filepath.Join(repoDir, ".cursor", "rules.md"), storageFile)
}

func TestProjectForget_LeavesForeignSymlinks(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, ".cursor/**", ".cursor/rules.md")
	storageFile := filepath.Join(svc.RepoPath(), "projects", id, ".cursor", "rules.md")

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	if err := os.Remove(liveFile); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "elsewhere")
	testhelpers.MakeFile(t, foreign, "not lnk\n")
	if err := os.Symlink(foreign, liveFile); err != nil {
		t.Fatal(err)
	}

	result, err := ps.ProjectForget(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectForget: %v", err)
	}
	if len(result.Unlinked) != 0 {
		t.Errorf("unlinked = %v, want empty for a foreign symlink", result.Unlinked)
	}

	target, err := os.Readlink(liveFile)
	if err != nil {
		t.Fatalf("expected foreign symlink to remain: %v", err)
	}
	if target != foreign {
		t.Errorf("symlink target = %q, want %q", target, foreign)
	}
	if !testhelpers.FileExists(t, storageFile) {
		t.Error("expected storage copy to be kept")
	}
}

func TestProjectForget_ErrorsWhenNotManaged(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	_, err := ps.ProjectForget(context.Background(), repoDir)
	if !errors.Is(err, lnkerror.ErrNotManaged) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNotManaged)
	}
}

func TestProjectRestore_GatesOnPatterns(t *testing.T) {
	svc, ps, repoDir, id := newPushedProject(t, "**/*.md", ".todo/a.md", "notes.md")
	storageDir := filepath.Join(svc.RepoPath(), "projects", id)

	// Hand-edit the manifest to drop the .todo pattern, leaving drift.
	manifest := filepath.Join(repoDir, ".lnkinclude")
	if err := os.WriteFile(manifest, []byte("notes.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	todoLive := filepath.Join(repoDir, ".todo", "a.md")
	notesLive := filepath.Join(repoDir, "notes.md")
	if err := os.Remove(todoLive); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(notesLive); err != nil {
		t.Fatal(err)
	}

	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 1 || info.Restored[0] != "notes.md" {
		t.Errorf("restored = %v, want [notes.md]", info.Restored)
	}
	if len(info.SkippedUnmatched) != 1 || info.SkippedUnmatched[0] != ".todo/a.md" {
		t.Errorf("skipped unmatched = %v, want [.todo/a.md]", info.SkippedUnmatched)
	}

	if _, err := os.Lstat(todoLive); err == nil {
		t.Error("expected unmatched stored file to stay unrestored")
	}
	testhelpers.AssertSymlink(t, notesLive, filepath.Join(storageDir, "notes.md"))
}

func TestProjectRestore_Idempotent(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".cursor", "rules.md")
	testhelpers.AssertSymlink(t, liveFile, storageFile)

	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 0 {
		t.Errorf("Restored = %v, want [] for already-correct symlink", info.Restored)
	}
	if len(info.BackedUp) != 0 {
		t.Errorf("BackedUp = %v, want []", info.BackedUp)
	}

	testhelpers.AssertSymlink(t, liveFile, storageFile)
}

func TestProjectRestoreHook_CreatesMissingAndReportsCollisions(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	rulesLive := filepath.Join(repoDir, ".cursor", "rules.md")
	todoLive := filepath.Join(repoDir, ".todo", "a.md")
	testhelpers.MakeFile(t, rulesLive, "# rules\n")
	testhelpers.MakeFile(t, todoLive, "- todo\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	todoStorage := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".todo", "a.md")

	// Simulate fresh clone: remove the symlinks.
	if err := os.Remove(rulesLive); err != nil {
		t.Fatalf("remove rules symlink: %v", err)
	}
	if err := os.Remove(todoLive); err != nil {
		t.Fatalf("remove todo symlink: %v", err)
	}

	// Put a real file at one target to create a collision.
	testhelpers.MakeFile(t, rulesLive, "local rules\n")

	info, err := ps.ProjectRestoreHook(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectRestoreHook: %v", err)
	}
	if len(info.Restored) != 1 || info.Restored[0] != ".todo/a.md" {
		t.Errorf("Restored = %v, want [.todo/a.md]", info.Restored)
	}
	if len(info.Collisions) != 1 || info.Collisions[0] != ".cursor/rules.md" {
		t.Errorf("Collisions = %v, want [.cursor/rules.md]", info.Collisions)
	}
	if len(info.BackedUp) != 0 {
		t.Errorf("BackedUp = %v, want []", info.BackedUp)
	}

	if _, err := os.Lstat(rulesLive + ".lnk-backup"); err == nil {
		t.Error("expected no .lnk-backup for hook collision")
	}
	content, err := os.ReadFile(rulesLive)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "local rules\n" {
		t.Errorf("collision file content = %q, want unchanged", string(content))
	}
	testhelpers.AssertSymlink(t, todoLive, todoStorage)
}

func TestProjectRestoreHook_Idempotent(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".cursor/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}

	liveFile := filepath.Join(repoDir, ".cursor", "rules.md")
	testhelpers.MakeFile(t, liveFile, "# rules\n")

	result, err := ps.ProjectPush(context.Background(), repoDir, false)
	if err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	storageFile := filepath.Join(svc.RepoPath(), "projects", result.ProjectID, ".cursor", "rules.md")
	testhelpers.AssertSymlink(t, liveFile, storageFile)

	info, err := ps.ProjectRestoreHook(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("ProjectRestoreHook: %v", err)
	}
	if len(info.Restored) != 0 {
		t.Errorf("Restored = %v, want [] for already-correct symlink", info.Restored)
	}
	if len(info.Collisions) != 0 {
		t.Errorf("Collisions = %v, want []", info.Collisions)
	}

	testhelpers.AssertSymlink(t, liveFile, storageFile)
}

// ---------- Global patterns, discovery, and doctor ----------

func TestProjectAddGlobalPattern(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)

	pattern, err := ps.ProjectAddGlobalPattern("AGENTS.md")
	if err != nil {
		t.Fatalf("ProjectAddGlobalPattern: %v", err)
	}
	if pattern != "AGENTS.md" {
		t.Errorf("pattern = %q, want AGENTS.md", pattern)
	}

	globalPath := filepath.Join(svc.RepoPath(), ".lnkinclude")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "AGENTS.md\n" {
		t.Errorf("global .lnkinclude = %q", data)
	}

	// Duplicate.
	if _, err := ps.ProjectAddGlobalPattern("AGENTS.md"); !errors.Is(err, lnkerror.ErrAlreadyManaged) {
		t.Errorf("duplicate error = %v, want %v", err, lnkerror.ErrAlreadyManaged)
	}

	// Empty.
	if _, err := ps.ProjectAddGlobalPattern("  "); !errors.Is(err, lnkerror.ErrEmptyPattern) {
		t.Errorf("empty error = %v, want %v", err, lnkerror.ErrEmptyPattern)
	}
}

func TestProjectUntrackGlobalPattern(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoDir := filepath.Join(home, "repos", "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, err := ps.ProjectAddGlobalPattern("AGENTS.md"); err != nil {
		t.Fatalf("add: %v", err)
	}

	removed, err := ps.ProjectUntrackGlobalPattern("AGENTS.md")
	if err != nil {
		t.Fatalf("ProjectUntrackGlobalPattern: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}

	globalPath := filepath.Join(svc.RepoPath(), ".lnkinclude")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "" {
		t.Errorf("global .lnkinclude = %q, want empty", data)
	}

	if _, err := ps.ProjectUntrackGlobalPattern("nope.md"); !errors.Is(err, lnkerror.ErrNotManaged) {
		t.Errorf("missing error = %v, want %v", err, lnkerror.ErrNotManaged)
	}
}

func TestProjectListProjects(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	// First project, two files.
	dir1 := filepath.Join(home, "repos", "alpha")
	testhelpers.MakeDir(t, dir1)
	initProjectRepo(t, dir1)
	ps1 := service.NewProjectService(svc)
	if _, _, err := ps1.ProjectAddPattern(context.Background(), dir1, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(dir1, ".todo", "a.md"), "a\n")
	testhelpers.MakeFile(t, filepath.Join(dir1, ".todo", "b.md"), "b\n")
	if _, err := ps1.ProjectPush(context.Background(), dir1, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	// Second project with a different origin, one file.
	dir2 := filepath.Join(home, "repos", "beta")
	testhelpers.MakeDir(t, dir2)
	testhelpers.InitGitRepo(t, dir2)
	if out, err := exec.Command("git", "-C", dir2, "remote", "add", "origin", "git@github.com:User/Other.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	ps2 := service.NewProjectService(svc)
	if _, _, err := ps2.ProjectAddPattern(context.Background(), dir2, "notes.md"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(dir2, "notes.md"), "notes\n")
	if _, err := ps2.ProjectPush(context.Background(), dir2, false); err != nil {
		t.Fatalf("ProjectPush: %v", err)
	}

	projects, err := ps1.ProjectListProjects()
	if err != nil {
		t.Fatalf("ProjectListProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %v, want 2", projects)
	}

	byID := map[string]int{}
	for _, p := range projects {
		byID[p.ID] = p.Files
	}
	if byID["github.com/user/repo"] != 2 {
		t.Errorf("github.com/user/repo files = %d, want 2", byID["github.com/user/repo"])
	}
	if byID["github.com/user/other"] != 1 {
		t.Errorf("github.com/user/other files = %d, want 1", byID["github.com/user/other"])
	}

	// Sorted by ID.
	if projects[0].ID > projects[1].ID {
		t.Errorf("projects not sorted: %v", projects)
	}
}

func TestProjectMarkerNotRestored(t *testing.T) {
	svc, ps, repoDir, _ := newPushedProject(t, ".cursor/**", ".cursor/rules.md")

	if err := os.Remove(filepath.Join(repoDir, ".cursor", "rules.md")); err != nil {
		t.Fatal(err)
	}
	info, err := ps.ProjectRestore(context.Background(), repoDir, false, false)
	if err != nil {
		t.Fatalf("ProjectRestore: %v", err)
	}
	if len(info.Restored) != 1 || info.Restored[0] != ".cursor/rules.md" {
		t.Errorf("restored = %v, want [.cursor/rules.md]", info.Restored)
	}
	if testhelpers.FileExists(t, filepath.Join(repoDir, ".lnkproject")) {
		t.Error("expected .lnkproject marker not to be restored into the project")
	}
	if !testhelpers.FileExists(t, filepath.Join(svc.RepoPath(), "projects", "github.com", "user", "repo", ".lnkproject")) {
		t.Error("expected .lnkproject marker in storage")
	}
}

func TestDoctor_ReportsStoredProjects(t *testing.T) {
	svc, _, _, _ := newPushedProject(t, ".todo/**", ".todo/a.md", ".todo/b.md")

	report, err := svc.Doctor(context.Background(), "", true, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(report.Projects) != 1 {
		t.Fatalf("projects = %v, want 1", report.Projects)
	}
	if report.Projects[0].ID != "github.com/user/repo" || report.Projects[0].Files != 2 {
		t.Errorf("project = %+v, want github.com/user/repo with 2 files", report.Projects[0])
	}
	if len(report.UnmarkedProjects) != 0 || len(report.EmptyProjects) != 0 {
		t.Errorf("unmarked = %v, empty = %v, want none", report.UnmarkedProjects, report.EmptyProjects)
	}
}

func TestDoctor_FlagsUnmarkedAndEmptyProjects(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	projectsRoot := filepath.Join(svc.RepoPath(), "projects")

	// Unmarked: files under projects/ with no marker anywhere.
	unmarkedDir := filepath.Join(projectsRoot, "legacy")
	testhelpers.MakeFile(t, filepath.Join(unmarkedDir, "x.md"), "x\n")

	// Empty: a marker but no stored files.
	emptyDir := filepath.Join(projectsRoot, "emptied")
	testhelpers.MakeDir(t, emptyDir)
	if err := os.WriteFile(filepath.Join(emptyDir, ".lnkproject"), []byte("emptied\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Doctor(context.Background(), "", true, false, false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(report.UnmarkedProjects) != 1 || report.UnmarkedProjects[0] != "legacy" {
		t.Errorf("unmarked = %v, want [legacy]", report.UnmarkedProjects)
	}
	if len(report.EmptyProjects) != 1 || report.EmptyProjects[0] != "emptied" {
		t.Errorf("empty = %v, want [emptied]", report.EmptyProjects)
	}
}

func TestDoctor_PrunesEmptyProjects(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	emptyDir := filepath.Join(svc.RepoPath(), "projects", "emptied")
	testhelpers.MakeDir(t, emptyDir)
	if err := os.WriteFile(filepath.Join(emptyDir, ".lnkproject"), []byte("emptied\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.CommitFile(t, svc.RepoPath(), "projects/emptied/.lnkproject")

	report, err := svc.Doctor(context.Background(), "", true, true, true)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(report.PrunedProjects) != 1 || report.PrunedProjects[0] != "emptied" {
		t.Errorf("pruned = %v, want [emptied]", report.PrunedProjects)
	}
	if testhelpers.FileExists(t, emptyDir) {
		t.Error("expected empty project storage to be removed")
	}
}
