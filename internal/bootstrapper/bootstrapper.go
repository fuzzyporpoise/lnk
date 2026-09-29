// Package bootstrapper handles bootstrap script discovery and execution.
package bootstrapper

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

type commandRunner func(ctx context.Context, name string, arg ...string) *exec.Cmd

type gitChecker interface {
	IsGitRepository() bool
}

// Runner handles bootstrap script discovery and execution.
type Runner struct {
	repoPath string
	git      gitChecker
	runCmd   commandRunner // defaults to exec.Command in constructor
}

// New creates a new bootstrap Runner.
func New(repoPath string, g gitChecker) *Runner {
	return &Runner{
		repoPath: repoPath,
		git:      g,
		runCmd:   exec.CommandContext,
	}
}

// FindScript searches for a bootstrap script in the repository.
func (r *Runner) FindScript() (string, error) {
	if !r.git.IsGitRepository() {
		return "", lnkerror.WithSuggestion(lnkerror.ErrNotInitialized, "run 'lnk init' first")
	}

	scriptPath := filepath.Join(r.repoPath, "bootstrap.sh")
	if _, err := os.Stat(scriptPath); err == nil {
		return "bootstrap.sh", nil
	}

	return "", nil
}

// RunScript executes the bootstrap script with configurable I/O.
func (r *Runner) RunScript(ctx context.Context, scriptName string, stdout, stderr io.Writer, stdin io.Reader) error {
	scriptPath := filepath.Join(r.repoPath, scriptName)
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		return lnkerror.Wrap(lnkerror.ErrBootstrapPerms)
	}

	cmd := r.runCmd(ctx, "bash", scriptPath)
	cmd.Dir = r.repoPath
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = stdin

	if err := cmd.Run(); err != nil {
		return lnkerror.WithSuggestion(lnkerror.ErrBootstrapFailed, err.Error())
	}

	return nil
}
