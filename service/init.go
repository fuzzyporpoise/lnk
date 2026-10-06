package service

import (
	"context"
	"fmt"
	"io"
	"os"

	bootpkg "go.fuzzyporpoise.dev/lnk/v2/internal/bootstrapper"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

// Init creates a new local repo when needed.
func (s *Service) Init(ctx context.Context) error {
	if err := os.MkdirAll(s.repoPath, 0o755); err != nil {
		return fmt.Errorf("failed to create lnk directory: %w", err)
	}

	if s.git.IsGitRepository() {
		if s.IsLnkRepository() {
			return nil
		}
		return lnkerror.WithPathAndSuggestion(lnkerror.ErrGitRepoExists, s.repoPath, "run 'lnk doctor --fix' to add lnkmarker to repo")
	}

	if err := s.git.Init(ctx); err != nil {
		return err
	}

	// Adds lnk marker for new repos only
	if err := s.writeMarkerFile(repoMarkerVersion); err != nil {
		return err
	}
	if err := s.stagePaths(ctx, repoMarkerFile); err != nil {
		return err
	}
	return s.commitAuto(ctx, "lnk: initialize repository")
}

// Clone clones a remote repo and optionally runs bootstrap.
func (s *Service) Clone(ctx context.Context, url string, runBootstrap bool, stdout, stderr io.Writer, stdin io.Reader) (bool, error) {
	if err := s.git.Clone(ctx, url); err != nil {
		return false, err
	}

	if !runBootstrap {
		return false, nil
	}

	runner := bootpkg.New(s.repoPath, s.git)
	script, err := runner.FindScript()
	if err != nil {
		return false, err
	}
	if script == "" {
		return false, nil
	}

	if err := runner.RunScript(ctx, script, stdout, stderr, stdin); err != nil {
		return true, err
	}
	return true, nil
}
