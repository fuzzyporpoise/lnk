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

// pushAndClone sets up a bare remote, pushes an initial lnk commit, then
// clones it to a new destination. Returns the cloned service and its repo path.
// This is the standard setup for Push/Pull/Update tests.
func pushAndClone(t *testing.T) (svc *service.Service, repoPath string) {
	t.Helper()
	remote := testhelpers.NewBareRemote(t)
	testhelpers.PushInitialCommit(t, remote)

	repoPath = filepath.Join(t.TempDir(), "cloned")
	svc = service.New(repoPath)
	if _, err := svc.Clone(context.Background(), remote, false, nil, nil, nil); err != nil {
		t.Fatalf("pushAndClone Clone: %v", err)
	}
	return svc, repoPath
}

// commitFileToRemote adds a file directly to the remote via the source repo
// and pushes it, so a subsequent Pull will bring it in.
func commitFileToRemote(t *testing.T, srcPath, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(srcPath, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"git", "-C", srcPath, "add", filename},
		{"git", "-C", srcPath, "commit", "-m", "add " + filename},
		{"git", "-C", srcPath, "push"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

// ---------- Commit tests ----------

func TestCommit_StagesAndCommitsAllChanges(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Write a file directly to repo storage without going through Add,
	// so there's an unstaged change to commit.
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	if err := os.MkdirAll(filepath.Dir(storagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, storagePath, "# bashrc")
	_ = home

	commitsBefore := testhelpers.GitLog(t, repoPath)

	if err := svc.Commit(context.Background(), "test: manual commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore)+1 {
		t.Errorf("expected 1 new commit, before=%d after=%d", len(commitsBefore), len(commitsAfter))
	}
	if !strings.Contains(commitsAfter[0], "test: manual commit") {
		t.Errorf("commit message = %q, want 'test: manual commit'", commitsAfter[0])
	}
}

func TestCommit_NoChanges(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	err := svc.Commit(context.Background(), "should fail")
	if err == nil {
		t.Fatal("expected error when no changes to commit, got nil")
	}
	if !errors.Is(err, lnkerror.ErrNoChanges) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrNoChanges)
	}
}

func TestCommit_UninitializedRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	svc := service.New(filepath.Join(home, ".config", "lnk"))
	err := svc.Commit(context.Background(), "should fail")
	if err == nil {
		t.Fatal("expected error for uninitialized repo, got nil")
	}
}

// ---------- Identity tests ----------

// gitAuthor returns the "Name <email>" of the latest commit.
func gitAuthor(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "log", "-1", "--format=%an <%ae>").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// localGitConfig returns the repo-local value for key, or "" when unset.
func localGitConfig(t *testing.T, repoPath, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "config", "--local", "--get", key).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestCommit_ManualUsesAmbientIdentity(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()
	testhelpers.ConfigureGitIdentity(t, repoPath)

	testhelpers.MakeFile(t, filepath.Join(repoPath, "common.lnk", ".bashrc"), "# bashrc")

	if err := svc.Commit(context.Background(), "manual"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if got, want := gitAuthor(t, repoPath), "Lnk Test <test@lnk>"; got != want {
		t.Errorf("author = %q, want %q", got, want)
	}
}

func TestAdd_AutomaticCommitUsesLnkIdentity(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()
	// Give the repo a distinct ambient identity: automatic commits must ignore it.
	testhelpers.ConfigureGitIdentity(t, repoPath)

	homeFile := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, homeFile, "# bashrc")

	if err := svc.Add(context.Background(), "", []string{homeFile}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got, want := gitAuthor(t, repoPath), "Lnk User <lnk@localhost>"; got != want {
		t.Errorf("author = %q, want %q", got, want)
	}
	// The automatic commit must leave the user's configured identity untouched.
	if got := localGitConfig(t, repoPath, "user.name"); got != "Lnk Test" {
		t.Errorf("local user.name = %q, want %q (automatic commit must not rewrite config)", got, "Lnk Test")
	}
}

// ---------- Status tests ----------

func TestStatus_CleanRepo_NoRemote(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.Dirty {
		t.Error("expected Dirty=false on clean repo")
	}
	if status.Remote != "" {
		t.Errorf("expected Remote=\"\" with no remote configured, got %q", status.Remote)
	}
	if status.Behind != 0 {
		t.Errorf("expected Behind=0 with no remote, got %d", status.Behind)
	}
}

func TestStatus_DirtyRepo(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()
	_ = home

	// Add an unstaged file to make the repo dirty.
	testhelpers.MakeFile(t, filepath.Join(repoPath, "common.lnk", ".bashrc"), "# bashrc")

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if !status.Dirty {
		t.Error("expected Dirty=true with unstaged changes")
	}
}

func TestStatus_AheadOfRemote(t *testing.T) {
	svc, repoPath := pushAndClone(t)
	testhelpers.ConfigureGitIdentity(t, repoPath)

	// Make a local commit without pushing.
	testhelpers.MakeFile(t, filepath.Join(repoPath, "local_file"), "content")
	cmds := [][]string{
		{"git", "-C", repoPath, "add", "local_file"},
		{"git", "-C", repoPath, "commit", "-m", "local commit"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.Ahead != 1 {
		t.Errorf("expected Ahead=1, got %d", status.Ahead)
	}
	if status.Behind != 0 {
		t.Errorf("expected Behind=0, got %d", status.Behind)
	}
}

func TestStatus_BehindRemote(t *testing.T) {
	remote := testhelpers.NewBareRemote(t)
	src := testhelpers.PushInitialCommit(t, remote)

	repoPath := filepath.Join(t.TempDir(), "cloned")
	svc := service.New(repoPath)
	if _, err := svc.Clone(context.Background(), remote, false, nil, nil, nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Push a new commit to remote from the source repo.
	commitFileToRemote(t, src, "remote_file", "from remote")

	// Fetch so the local clone knows about the remote change. Status reports
	// Behind based on the local remote tracking ref, not the live remote state.
	if out, err := exec.Command("git", "-C", repoPath, "fetch", "origin").CombinedOutput(); err != nil {
		t.Fatalf("git fetch: %v\n%s", err, out)
	}

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.Behind != 1 {
		t.Errorf("expected Behind=1, got %d", status.Behind)
	}
}

func TestStatus_UninitializedRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	svc := service.New(filepath.Join(home, ".config", "lnk"))
	_, err := svc.Status(context.Background())
	if err == nil {
		t.Fatal("expected error for uninitialized repo, got nil")
	}
}

// ---------- Diff tests ----------

func TestDiff_CleanRepo(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	diff, err := svc.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff on clean repo, got %q", diff)
	}
}

func TestDiff_WithChanges(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()
	_ = home

	// Stage a file so there's something to diff.
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.MakeFile(t, storagePath, "# bashrc")
	if out, err := exec.Command("git", "-C", repoPath, "add", storagePath).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	// Modify after staging to create a diff.
	testhelpers.MakeFile(t, storagePath, "# bashrc modified")

	diff, err := svc.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff with modified file")
	}
	if !strings.Contains(diff, ".bashrc") {
		t.Errorf("diff = %q, want mention of .bashrc", diff)
	}
}

func TestDiff_UninitializedRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	svc := service.New(filepath.Join(home, ".config", "lnk"))
	_, err := svc.Diff(context.Background())
	if err == nil {
		t.Fatal("expected error for uninitialized repo, got nil")
	}
}

// ---------- Push tests ----------

func TestPush_Success(t *testing.T) {
	svc, repoPath := pushAndClone(t)
	testhelpers.ConfigureGitIdentity(t, repoPath)

	// Make a local commit to push.
	testhelpers.MakeFile(t, filepath.Join(repoPath, "local_file"), "content")
	cmds := [][]string{
		{"git", "-C", repoPath, "add", "local_file"},
		{"git", "-C", repoPath, "commit", "-m", "local commit"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	if err := svc.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// After push, Ahead should be 0.
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status after push: %v", err)
	}
	if status.Ahead != 0 {
		t.Errorf("expected Ahead=0 after push, got %d", status.Ahead)
	}
}

func TestPush_DirtyRepo(t *testing.T) {
	svc, repoPath := pushAndClone(t)

	// Leave an unstaged change — push should refuse.
	testhelpers.MakeFile(t, filepath.Join(repoPath, "dirty_file"), "content")

	err := svc.Push(context.Background())
	if err == nil {
		t.Fatal("expected error pushing with dirty working tree, got nil")
	}
	if !errors.Is(err, lnkerror.ErrDirtyTree) {
		t.Errorf("error = %v, want %v", err, lnkerror.ErrDirtyTree)
	}
}

func TestPush_NoRemote(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	err := svc.Push(context.Background())
	if err == nil {
		t.Fatal("expected error pushing with no remote configured, got nil")
	}
}

// ---------- Pull tests ----------

func TestPull_BringsInRemoteChanges(t *testing.T) {
	remote := testhelpers.NewBareRemote(t)
	src := testhelpers.PushInitialCommit(t, remote)

	repoPath := filepath.Join(t.TempDir(), "cloned")
	svc := service.New(repoPath)
	if _, err := svc.Clone(context.Background(), remote, false, nil, nil, nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Push a new file to remote from the source repo.
	commitFileToRemote(t, src, "remote_file", "from remote")

	if err := svc.Pull(context.Background()); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if !testhelpers.FileExists(t, filepath.Join(repoPath, "remote_file")) {
		t.Error("expected remote_file to exist after pull")
	}
}

func TestPull_NoRemote(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	err := svc.Pull(context.Background())
	if err == nil {
		t.Fatal("expected error pulling with no remote configured, got nil")
	}
}

// ---------- Update tests ----------

func TestUpdate_PullsThenRestores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	remote := testhelpers.NewBareRemote(t)
	src := testhelpers.PushInitialCommit(t, remote)

	repoPath := filepath.Join(home, ".config", "lnk")
	svc := service.New(repoPath)
	if _, err := svc.Clone(context.Background(), remote, false, nil, nil, nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Push a tracked file to remote — write it into common.lnk/ and update
	// the tracker so Restore has something to do after Pull.
	commonLnk := filepath.Join(src, "common.lnk")
	if err := os.MkdirAll(commonLnk, 0o755); err != nil {
		t.Fatal(err)
	}
	testhelpers.MakeFile(t, filepath.Join(commonLnk, ".bashrc"), "# bashrc")
	cmds := [][]string{
		{"git", "-C", src, "add", "."},
		{"git", "-C", src, "commit", "-m", "add .bashrc to common"},
		{"git", "-C", src, "push"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	// Also write the tracker entry so Restore knows about .bashrc.
	trackerSrc := filepath.Join(src, ".lnk.common")
	if err := os.WriteFile(trackerSrc, []byte(".bashrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds = [][]string{
		{"git", "-C", src, "add", ".lnk.common"},
		{"git", "-C", src, "commit", "-m", "track .bashrc"},
		{"git", "-C", src, "push"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	info, err := svc.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Pull should have brought in .bashrc from remote.
	if !testhelpers.FileExists(t, filepath.Join(repoPath, "common.lnk", ".bashrc")) {
		t.Error("expected .bashrc in repo storage after pull")
	}

	// Restore should have created the symlink at the live path.
	livePath := filepath.Join(home, ".bashrc")
	storagePath := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, livePath, storagePath)

	if len(info.Restored) != 1 || info.Restored[0] != ".bashrc" {
		t.Errorf("Restored = %v, want [.bashrc]", info.Restored)
	}
}

func TestUpdate_NoRemote(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)

	_, err := svc.Update(context.Background(), "")
	if err == nil {
		t.Fatal("expected error updating with no remote configured, got nil")
	}
}
