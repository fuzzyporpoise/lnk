// Package testhelpers provides utilities for setting up lnk test environments.
package testhelpers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// ---------- Repo setup helpers ----------

// NewTestRepo creates a temporary directory and returns a Service pointed at it.
func NewTestRepo(t *testing.T) (svc *service.Service, repoPath string) {
	t.Helper()
	repoPath = t.TempDir()
	svc = service.New(repoPath)
	return svc, repoPath
}

// InitRepo is a convenience that calls Init and fails the test on error.
func InitRepo(t *testing.T, svc *service.Service) {
	t.Helper()
	if err := svc.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
}

// InitGitRepo initializes a git repository at dir with a default test config.
func InitGitRepo(t *testing.T, dir string) {
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

// TestHome sets up a temp home directory and an initialized v2 lnk repo inside it,
// returning the service and the home path. The repo lives at $HOME/.config/lnk
// to match the default ResolveRepoPath behaviour.
func TestHome(t *testing.T) (svc *service.Service, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "") // ensure default XDG path is used
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LNK_REGISTRY", "") // ignore any developer override

	repoPath := filepath.Join(home, ".config", "lnk")
	svc = service.New(repoPath)
	if err := svc.Init(context.Background()); err != nil {
		t.Fatalf("TestHome Init: %v", err)
	}
	return svc, home
}

// TestHomeV1 sets up a temp home directory with a v1-format lnk repo built
// directly on disk — no Init or Format call — so tests don't depend on Format.
// v1 layout: files stored in repo root, tracker is .lnk, marker is version=1.
func TestHomeV1(t *testing.T) (svc *service.Service, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	repoPath := filepath.Join(home, ".config", "lnk")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	gitCmds := [][]string{
		{"git", "-C", repoPath, "init", "-b", "main"},
		{"git", "-C", repoPath, "config", "user.email", "test@lnk"},
		{"git", "-C", repoPath, "config", "user.name", "Lnk Test"},
	}
	runGitCmds(t, gitCmds)

	markerPath := filepath.Join(repoPath, ".lnkrepo")
	if err := os.WriteFile(markerPath, []byte("version=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitCmds := [][]string{
		{"git", "-C", repoPath, "add", ".lnkrepo"},
		{"git", "-C", repoPath, "commit", "-m", "lnk: initialize repository"},
	}
	runGitCmds(t, commitCmds)

	return service.New(repoPath), home
}

// TestHomeV1Legacy sets up a temp home directory with a v1 repo without a
// .lnkrepo marker file, simulating a legacy config created before the marker
// was introduced. FindVersion falls back to detecting the .lnk tracker file
// on disk to determine format.
func TestHomeV1Legacy(t *testing.T) (svc *service.Service, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	repoPath := filepath.Join(home, ".config", "lnk")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	gitCmds := [][]string{
		{"git", "-C", repoPath, "init", "-b", "main"},
		{"git", "-C", repoPath, "config", "user.email", "test@lnk"},
		{"git", "-C", repoPath, "config", "user.name", "Lnk Test"},
	}
	runGitCmds(t, gitCmds)

	lnkPath := filepath.Join(repoPath, ".lnk")
	if err := os.WriteFile(lnkPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	commitCmds := [][]string{
		{"git", "-C", repoPath, "add", ".lnk"},
		{"git", "-C", repoPath, "commit", "-m", "lnk: initialize repository"},
	}
	runGitCmds(t, commitCmds)

	return service.New(repoPath), home
}

// NewBareRemote creates a bare git repo to act as a remote.
func NewBareRemote(t *testing.T) string {
	t.Helper()
	remote := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return remote
}

// PushInitialCommit initialises a source repo, commits the lnk marker, and
// pushes to remote. Returns the source repo path so callers can add more
// commits before cloning.
func PushInitialCommit(t *testing.T, remote string) string {
	t.Helper()
	src := t.TempDir()
	cmds := [][]string{
		{"git", "-C", src, "init", "-b", "main"},
		{"git", "-C", src, "config", "user.email", "test@lnk"},
		{"git", "-C", src, "config", "user.name", "Lnk Test"},
		{"git", "-C", src, "remote", "add", "origin", remote},
	}
	runGitCmds(t, cmds)

	if err := os.WriteFile(filepath.Join(src, ".lnkrepo"), []byte("version=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds = [][]string{
		{"git", "-C", src, "add", ".lnkrepo"},
		{"git", "-C", src, "commit", "-m", "lnk: initialize repository"},
		{"git", "-C", src, "push", "-u", "origin", "main"},
	}
	runGitCmds(t, cmds)
	return src
}

// ---------- Filesystem helpers ----------

// FileExists reports whether path exists.
func FileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// MakeFile creates a file at the given absolute path with the given content.
func MakeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// MakeDir creates a directory at the given path.
func MakeDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// AssertSymlink checks that path is a symlink pointing to expectedTarget.
func AssertSymlink(t *testing.T, path, expectedTarget string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%q is not a symlink", path)
		return
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("Readlink(%q): %v", path, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	absTarget, _ := filepath.Abs(target)
	absExpected, _ := filepath.Abs(expectedTarget)
	if absTarget != absExpected {
		t.Errorf("symlink %q -> %q, want %q", path, absTarget, absExpected)
	}
}

// ---------- Tracker helpers ----------

// AssertTracked checks that relativePath appears in the common tracker file.
func AssertTracked(t *testing.T, repoPath, relativePath string) {
	t.Helper()
	AssertTrackedInScope(t, repoPath, tracker.CommonScope, relativePath)
}

// AssertNotTracked checks that relativePath does not appear in the common tracker.
func AssertNotTracked(t *testing.T, repoPath, relativePath string) {
	t.Helper()
	AssertNotTrackedInScope(t, repoPath, tracker.CommonScope, relativePath)
}

// AssertTrackedInScope checks that relativePath appears in the tracker for scope.
func AssertTrackedInScope(t *testing.T, repoPath, scope, relativePath string) {
	t.Helper()
	content := ReadTrackerForScope(t, repoPath, scope)
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == relativePath {
			return
		}
	}
	t.Errorf("relative path %q not found in tracker for scope %q", relativePath, scope)
}

// AssertNotTrackedInScope checks that relativePath does not appear in the
// tracker for the given scope.
func AssertNotTrackedInScope(t *testing.T, repoPath, scope, relativePath string) {
	t.Helper()
	content := ReadTrackerForScope(t, repoPath, scope)
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == relativePath {
			t.Errorf("relative path %q unexpectedly found in tracker for scope %q", relativePath, scope)
			return
		}
	}
}

// ReadTrackerForScope returns the contents of the tracker file for a scope.
func ReadTrackerForScope(t *testing.T, repoPath, scope string) string {
	t.Helper()
	var candidates []string
	if scope == "" || scope == tracker.CommonScope {
		candidates = []string{".lnk.common", ".lnk"}
	} else {
		candidates = []string{".lnk." + scope}
	}
	for _, name := range candidates {
		content, err := os.ReadFile(filepath.Join(repoPath, name))
		if err == nil {
			return string(content)
		}
	}
	return ""
}

// ---------- Git helpers ----------

// runGitCmds runs a sequence of git commands, failing the test on the first error.
func runGitCmds(t *testing.T, cmds [][]string) {
	t.Helper()
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

// GitLog returns the one-line git log for the repo at path.
func GitLog(t *testing.T, repoPath string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "log", "--oneline").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// CommitEmptyScope writes an empty tracker file for a host scope and commits
// it to git, simulating a scope that was left empty after all files were moved
// out. This is needed for prune tests since git add -A only works for files
// already tracked by git.
func CommitEmptyScope(t *testing.T, repoPath, host string) {
	t.Helper()
	trackerPath := filepath.Join(repoPath, ".lnk."+host)
	if err := os.WriteFile(trackerPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"git", "-C", repoPath, "add", ".lnk." + host},
		{"git", "-C", repoPath, "commit", "-m", "lnk: empty scope " + host},
	}
	runGitCmds(t, cmds)
}

// RunGit executes a git command inside repoPath for integration tests.
func RunGit(t *testing.T, repoPath string, args ...string) error {
	t.Helper()
	fullArgs := append([]string{"-C", repoPath}, args...)
	out, err := RunCmd(t, "git", fullArgs...)
	if err != nil {
		t.Logf("git %v: %s", args, out)
	}
	return err
}

// CommitDeletion stages and commits the deletion of a file previously tracked
// by git. Used in doctor tests where setup deletes a file from storage before
// calling Doctor --fix, which requires a clean working tree.
func CommitDeletion(t *testing.T, repoPath, relativePath string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", repoPath, "rm", "--ignore-unmatch", relativePath},
		{"git", "-C", repoPath, "commit", "-m", "test: remove " + relativePath},
	}
	runGitCmds(t, cmds)
}

// CommitFile stages and commits a file that was written directly to disk.
// Used when test setup writes files outside of service methods.
func CommitFile(t *testing.T, repoPath, relativePath string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", repoPath, "add", relativePath},
		{"git", "-C", repoPath, "commit", "-m", "test: add " + relativePath},
	}
	runGitCmds(t, cmds)
}

// ConfigureGitIdentity sets git identity so that tests work without relying on
// global git config, which may be absent in CI.
func ConfigureGitIdentity(t *testing.T, repoPath string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", repoPath, "config", "user.email", "test@lnk"},
		{"git", "-C", repoPath, "config", "user.name", "Lnk Test"},
	}
	runGitCmds(t, cmds)
}

// ---------- Shell helpers ----------

// RunCmd executes a shell command and reports failures using *testing.T.
func RunCmd(t *testing.T, name string, args ...string) ([]byte, error) {
	t.Helper() // Marks function as a helper so error line numbers trace back correctly

	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}
