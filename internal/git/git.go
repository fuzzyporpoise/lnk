// Package git provides Git operations for lnk.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

// Sentinel errors for git operations.
var (
	ErrGitInit     = errors.New("git init failed")
	ErrBranchSetup = errors.New("failed to set up the default branch")
	ErrGitCommand  = errors.New("git operation failed")
	ErrNoRemote    = errors.New("no remote repository is configured")
	ErrPush        = errors.New("failed to push changes to remote repository")
	ErrPull        = errors.New("failed to pull changes from remote repository")
	ErrGitTimeout  = errors.New("git operation timed out")
	ErrDirRemove   = errors.New("failed to prepare directory for operation")
	ErrDirCreate   = errors.New("failed to create directory")
	ErrUncommitted = errors.New("git repo has uncommitted changes")
	ErrDiff        = errors.New("failed to get diff output")
)

const (
	// shortTimeout for fast local operations (status, add, commit, etc.)
	shortTimeout = 30 * time.Second

	// longTimeout for network operations and large transfers (clone, push, pull)
	longTimeout = 5 * time.Minute
)

// Git handles Git operations
type Git struct {
	repoPath string
	color    bool
}

type Option func(*Git)

// WithColor sets color.ui=always for git commmands
func WithColor() Option {
	return func(g *Git) { g.color = true }
}

func New(repoPath string, opts ...Option) *Git {
	g := &Git{
		repoPath: repoPath,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// runGitCommand executes a git command with the given timeout.
func (g *Git) runGitCommand(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmdArgs []string
	if g.color {
		cmdArgs = append(cmdArgs, "-c", "color.ui=always")
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Dir = g.repoPath
	return cmd.CombinedOutput()
}

// Run executes an arbitrary read-only git command against the repository and
// returns its combined output.
func (g *Git) Run(ctx context.Context, args ...string) ([]byte, error) {
	return g.runGitCommand(ctx, shortTimeout, args...)
}

// Init initializes a new Git repository
func (g *Git) Init(ctx context.Context) error {
	// Try using git init -b main first (Git 2.28+)
	_, err := g.runGitCommand(ctx, shortTimeout, "init", "-b", "main")
	if err != nil {
		// Fallback to regular init + branch rename for older Git versions
		_, err := g.runGitCommand(ctx, shortTimeout, "init")
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return lnkerror.WithSuggestion(ErrGitTimeout, "check system resources and try again")
			}
			return lnkerror.WithSuggestion(ErrGitInit, "ensure git is installed and try again")
		}

		// Set the default branch to main
		_, err = g.runGitCommand(ctx, shortTimeout, "symbolic-ref", "HEAD", "refs/heads/main")
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return lnkerror.WithSuggestion(ErrGitTimeout, "check system resources and try again")
			}
			return lnkerror.WithSuggestion(ErrBranchSetup, "check your git installation")
		}
	}

	return nil
}

// getRemoteURL returns the URL for a remote, or error if not found
func (g *Git) getRemoteURL(ctx context.Context, name string) (string, error) {
	output, err := g.runGitCommand(ctx, shortTimeout, "remote", "get-url", name)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", lnkerror.Wrap(ErrGitTimeout)
		}
		return "", lnkerror.Wrap(ErrGitCommand)
	}

	return strings.TrimSpace(string(output)), nil
}

// IsGitRepository checks if the directory contains a Git repository
func (g *Git) IsGitRepository() bool {
	gitDir := filepath.Join(g.repoPath, ".git")
	_, err := os.Stat(gitDir)
	return err == nil
}

// Stage adds a path to the index, or stages its removal if it no longer exists.
func (g *Git) Stage(ctx context.Context, path string) error {
	fullPath := filepath.Join(g.repoPath, path)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		// File was deleted — stage the removal. --ignore-unmatch avoids
		// erroring if the file was never tracked.
		_, err := g.runGitCommand(ctx, shortTimeout, "rm", "--ignore-unmatch", path)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return lnkerror.Wrap(ErrGitTimeout)
			}
			return lnkerror.WithSuggestion(ErrGitCommand, "check file permissions and try again")
		}
		return nil
	}

	// File exists — stage normally.
	_, err := g.runGitCommand(ctx, shortTimeout, "add", "-A", path)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "check file permissions and try again")
	}
	return nil
}

// Machine identity used for lnk's automatic commits. It is supplied as
// per-invocation -c arguments rather than written to any git config, so it
// never shadows the user's own identity.
const (
	LnkCommitName  = "Lnk User"
	LnkCommitEmail = "lnk@localhost"
)

// Commit creates a commit using the ambient git identity (from the
// environment or git config). When no identity resolves (an environment where
// git would refuse to commit), it falls back to the lnk machine identity via
// per-invocation -c arguments so the commit still succeeds without touching
// any git config.
func (g *Git) Commit(ctx context.Context, message string) error {
	if !g.hasIdentity(ctx) {
		return g.CommitAs(ctx, LnkCommitName, LnkCommitEmail, message)
	}
	_, err := g.runGitCommand(ctx, shortTimeout, "commit", "-m", message)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "ensure you have staged changes and try again")
	}

	return nil
}

// CommitAs creates a commit authored with the given identity, passed as
// per-invocation -c arguments so the persistent git config is never modified.
// Signing is disabled so machine commits are never signed with the user's key,
// which may be an interactive or unavailable key in unattended runs.
//
// When paths are given, the commit is limited to the current state of those
// paths, ignoring anything else staged in the index.
func (g *Git) CommitAs(ctx context.Context, name, email, message string, paths ...string) error {
	args := []string{
		"-c", "user.name=" + name,
		"-c", "user.email=" + email,
		"-c", "commit.gpgSign=false",
		"commit", "-m", message,
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	_, err := g.runGitCommand(ctx, shortTimeout, args...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "ensure you have staged changes and try again")
	}

	return nil
}

// hasIdentity reports whether git can resolve a full author and committer
// identity from the environment or config, i.e. whether a plain `git commit`
// would proceed without an explicit -c identity. Each of author and committer
// resolves name from GIT_<ROLE>_NAME or user.name, and email from
// GIT_<ROLE>_EMAIL, user.email, or EMAIL; both roles must resolve.
func (g *Git) hasIdentity(ctx context.Context) bool {
	hasName := g.configValue(ctx, "user.name") != "" ||
		(os.Getenv("GIT_COMMITTER_NAME") != "" && os.Getenv("GIT_AUTHOR_NAME") != "")
	hasEmail := g.configValue(ctx, "user.email") != "" || os.Getenv("EMAIL") != "" ||
		(os.Getenv("GIT_COMMITTER_EMAIL") != "" && os.Getenv("GIT_AUTHOR_EMAIL") != "")
	return hasName && hasEmail
}

// configValue returns a config value from any level, or "" when the key is
// unset or unreadable. It is only used for probing; callers that need error
// detail should use localConfig or runGitCommand directly.
func (g *Git) configValue(ctx context.Context, key string) string {
	out, err := g.runGitCommand(ctx, shortTimeout, "config", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// LocalIdentity returns the user.name and user.email stored in the
// repository's local config only (empty when a key is unset). Global and
// system values are ignored.
func (g *Git) LocalIdentity(ctx context.Context) (name, email string, err error) {
	if name, err = g.localConfig(ctx, "user.name"); err != nil {
		return "", "", err
	}
	if email, err = g.localConfig(ctx, "user.email"); err != nil {
		return "", "", err
	}
	return name, email, nil
}

// localConfig reads a single key from the repository's local config. An unset
// key is not an error; it yields an empty string.
func (g *Git) localConfig(ctx context.Context, key string) (string, error) {
	out, err := g.runGitCommand(ctx, shortTimeout, "config", "--local", "--get", key)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", lnkerror.Wrap(ErrGitTimeout)
		}
		// Exit code 1 means the key is unset, which is not an error here.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", lnkerror.WithSuggestion(ErrGitCommand, "check your git installation")
	}
	return strings.TrimSpace(string(out)), nil
}

// UnsetLocalIdentity removes user.name and user.email from the repository's
// local config. It is used to clear the identity that older lnk versions
// mistakenly persisted, restoring the user's ambient identity. Missing keys
// are ignored.
func (g *Git) UnsetLocalIdentity(ctx context.Context) error {
	for _, key := range []string{"user.name", "user.email"} {
		_, err := g.runGitCommand(ctx, shortTimeout, "config", "--local", "--unset", key)
		if err == nil {
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		// Exit code 5 means the key was not set, which is fine.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
			continue
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "check your git installation")
	}
	return nil
}

// getRemoteInfo returns information about the default remote.
func (g *Git) getRemoteInfo(ctx context.Context) (string, error) {
	// First try to get origin remote
	url, err := g.getRemoteURL(ctx, "origin")
	if err != nil {
		// If origin doesn't exist, try to get any remote
		output, err := g.runGitCommand(ctx, shortTimeout, "remote")
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return "", lnkerror.Wrap(ErrGitTimeout)
			}
			return "", lnkerror.Wrap(ErrGitCommand)
		}

		remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(remotes) == 0 || remotes[0] == "" {
			return "", lnkerror.WithSuggestion(ErrNoRemote, "add a remote repository first")
		}

		// Use the first remote
		url, err = g.getRemoteURL(ctx, remotes[0])
		if err != nil {
			return "", lnkerror.WithPath(ErrNoRemote, remotes[0])
		}
	}

	return url, nil
}

// StatusInfo contains repository status information.
// Output is the raw human-readable git status text.
// Parsed fields are extracted for programmatic use.
type StatusInfo struct {
	Output string // Raw git status --short (or --long) output
	Dirty  bool   // True if there are any uncommitted changes
	Ahead  int    // Commits ahead of upstream (0 if no upstream)
	Behind int    // Commits behind upstream (0 if no upstream)
	Remote string // Upstream branch name or empty if none
}

// GetStatus returns the repository status.
// Output contains the full git status text (with color if requested).
// When no remote is configured, Remote is empty and Ahead counts local commits.
func (g *Git) GetStatus(ctx context.Context) (*StatusInfo, error) {
	// Get machine-readable dirty state
	porcelain, err := g.runGitCommand(ctx, shortTimeout, "status", "--porcelain")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, lnkerror.Wrap(ErrGitTimeout)
		}
		return nil, lnkerror.Wrap(ErrGitCommand)
	}

	// Get human-readable output with color config
	output, err := g.runGitCommand(ctx, shortTimeout, "status")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, lnkerror.Wrap(ErrGitTimeout)
		}
		return nil, lnkerror.Wrap(ErrGitCommand)
	}

	status := &StatusInfo{
		Output: string(output),
		Dirty:  len(strings.TrimSpace(string(porcelain))) > 0,
	}

	// Check if we have a remote
	_, remoteErr := g.getRemoteInfo(ctx)
	if remoteErr != nil {
		if errors.Is(remoteErr, ErrNoRemote) {
			// Local-only repo: count all commits, mark dirty from output
			status.Ahead = g.getLocalCommitCount(ctx)
			status.Behind = 0
			status.Remote = ""
			return status, nil
		}
		return nil, remoteErr
	}

	// Get upstream branch
	upstreamOutput, err := g.runGitCommand(ctx, shortTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, lnkerror.Wrap(ErrGitTimeout)
		}
		// No upstream set
		status.Ahead = g.getLocalCommitCount(ctx)
		status.Behind = 0
		status.Remote = "origin/main" // default assumption
		return status, nil
	}

	remoteBranch := strings.TrimSpace(string(upstreamOutput))
	status.Remote = remoteBranch
	status.Ahead = g.getAheadCount(ctx, remoteBranch)
	status.Behind = g.getBehindCount(ctx, remoteBranch)

	// Append ahead/behind summary to output for convenience
	if status.Ahead > 0 || status.Behind > 0 {
		status.Output += fmt.Sprintf("\n[remote:%s - ahead:%d behind:%d]\n", remoteBranch, status.Ahead, status.Behind)
	}

	return status, nil
}

// getLocalCommitCount returns the total number of commits on HEAD, or 0 if
// there are no commits yet (fresh repo).
func (g *Git) getLocalCommitCount(ctx context.Context) int {
	output, err := g.runGitCommand(ctx, shortTimeout, "rev-list", "--count", "HEAD")
	if err != nil {
		return 0
	}
	count := strings.TrimSpace(string(output))
	if count == "" {
		return 0
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}
	return n
}

// getAheadCount returns how many commits ahead of remote
func (g *Git) getAheadCount(ctx context.Context, remoteBranch string) int {
	var output []byte

	output, err := g.runGitCommand(ctx, shortTimeout, "rev-list", "--count", fmt.Sprintf("%s..HEAD", remoteBranch))
	if err != nil {
		// If remote branch doesn't exist, count all local commits
		output, err = g.runGitCommand(ctx, shortTimeout, "rev-list", "--count", "HEAD")
		if err != nil {
			return 0
		}
	}

	count := strings.TrimSpace(string(output))
	if count == "" {
		return 0
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}
	return n
}

// getBehindCount returns how many commits behind remote
func (g *Git) getBehindCount(ctx context.Context, remoteBranch string) int {
	output, err := g.runGitCommand(ctx, shortTimeout, "rev-list", "--count", fmt.Sprintf("HEAD..%s", remoteBranch))
	if err != nil {
		return 0
	}

	count := strings.TrimSpace(string(output))
	if count == "" {
		return 0
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}
	return n
}

// RmCached removes paths from the git index while preserving the working-tree
// copies. When recursive is true, it passes -r to handle directories.
func (g *Git) RmCached(ctx context.Context, paths []string, recursive bool) error {
	args := []string{"rm", "--cached"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, paths...)

	_, err := g.runGitCommand(ctx, shortTimeout, args...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "check file permissions and try again")
	}
	return nil
}

// HasChanges checks if there are uncommitted changes. When paths are given,
// only changes under those paths are considered, which keeps automatic commits
// limited to the subtree they touched.
func (g *Git) HasChanges(ctx context.Context, paths ...string) (bool, error) {
	args := []string{"status", "--porcelain"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	output, err := g.runGitCommand(ctx, shortTimeout, args...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return false, lnkerror.Wrap(ErrGitTimeout)
		}
		return false, lnkerror.Wrap(ErrGitCommand)
	}

	return len(strings.TrimSpace(string(output))) > 0, nil
}

// HasStagedChanges checks if there are staged changes
func (g *Git) HasStagedChanges(ctx context.Context) (bool, error) {
	output, err := g.runGitCommand(ctx, shortTimeout, "diff", "--cached", "--stat")
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// Diff returns the diff output for uncommitted changes in the repository.
// If color is true, the output will include ANSI color codes.
func (g *Git) Diff(ctx context.Context) (string, error) {

	output, err := g.runGitCommand(ctx, shortTimeout, "diff")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", lnkerror.Wrap(ErrGitTimeout)
		}
		return "", lnkerror.Wrap(ErrDiff)
	}

	return string(output), nil
}

// AddAll stages all changes in the repository
func (g *Git) AddAll(ctx context.Context) error {
	_, err := g.runGitCommand(ctx, shortTimeout, "add", "-A")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "check file permissions and try again")
	}

	return nil
}

// Push pushes changes to remote
func (g *Git) Push(ctx context.Context) error {
	// First ensure we have a remote configured
	_, err := g.getRemoteInfo(ctx)
	if err != nil {
		return lnkerror.WithSuggestion(ErrPush, err.Error())
	}

	// Explicit HEAD refspec: without it, push fails with exit 128 when the
	// branch has no upstream (fresh init + remote add) on any git config
	// lacking push.autoSetupRemote.
	_, err = g.runGitCommand(ctx, longTimeout, "push", "-u", "origin", "HEAD")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrPush, err.Error())
	}

	return nil
}

// Pull pulls changes from remote
func (g *Git) Pull(ctx context.Context) error {
	// First ensure we have a remote configured
	_, err := g.getRemoteInfo(ctx)
	if err != nil {
		return lnkerror.WithSuggestion(ErrPull, err.Error())
	}

	_, err = g.runGitCommand(ctx, longTimeout, "pull", "origin")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrPull, err.Error())
	}

	return nil
}

// Clone clones a repository from the given URL
func (g *Git) Clone(ctx context.Context, url string) error {
	// Remove the directory if it exists to ensure clean clone
	if err := os.RemoveAll(g.repoPath); err != nil {
		return lnkerror.WithPathAndSuggestion(ErrDirRemove, g.repoPath, err.Error())
	}

	// Create parent directory
	parentDir := filepath.Dir(g.repoPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return lnkerror.WithPathAndSuggestion(ErrDirCreate, parentDir, err.Error())
	}

	// Clone the repository
	// Note: Can't use runGitCommand here because it sets cmd.Dir to g.repoPath,
	// which doesn't exist yet. Clone needs to run from parent directory.
	ctx, cancel := context.WithTimeout(ctx, longTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", url, g.repoPath)
	_, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		return lnkerror.WithSuggestion(ErrGitCommand, "check the repository URL and your network connection")
	}

	// Set up upstream tracking for main branch
	_, err = g.runGitCommand(ctx, shortTimeout, "branch", "--set-upstream-to=origin/main", "main")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return lnkerror.Wrap(ErrGitTimeout)
		}
		// If main doesn't exist, try master
		_, err = g.runGitCommand(ctx, shortTimeout, "branch", "--set-upstream-to=origin/master", "master")
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return lnkerror.Wrap(ErrGitTimeout)
			}
			// If that also fails, try to set upstream for current branch
			_, err = g.runGitCommand(ctx, shortTimeout, "branch", "--set-upstream-to=origin/HEAD")
			if err != nil {
				return lnkerror.Wrap(ErrGitCommand)
			}
		}
	}

	return nil
}
