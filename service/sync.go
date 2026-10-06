package service

import (
	"context"

	gitpkg "go.fuzzyporpoise.dev/lnk/v2/internal/git"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

// Commit stages all repo changes and creates a commit using the ambient git
// identity (the user's own).
func (s *Service) Commit(ctx context.Context, message string) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}
	hasChanges, err := s.git.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges {
		return lnkerror.Wrap(lnkerror.ErrNoChanges)
	}
	if err := s.git.AddAll(ctx); err != nil {
		return err
	}
	return s.git.Commit(ctx, message)
}

// Push pushes existing commits only.
func (s *Service) Push(ctx context.Context) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}
	hasChanges, err := s.git.HasChanges(ctx)
	if err != nil {
		return err
	}
	if hasChanges {
		return lnkerror.WithSuggestion(lnkerror.ErrDirtyTree, "run 'lnk commit' or commit manually before push")
	}
	return s.git.Push(ctx)
}

// Pull updates the repo only.
func (s *Service) Pull(ctx context.Context) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}
	return s.git.Pull(ctx)
}

// Update pulls repo changes and then restores the effective machine profile.
func (s *Service) Update(ctx context.Context, host string) (RestoreInfo, error) {
	if err := s.Pull(ctx); err != nil {
		return RestoreInfo{}, err
	}
	return s.Restore(ctx, host, false)
}

// Status returns repo status information.
func (s *Service) Status(ctx context.Context) (*gitpkg.StatusInfo, error) {
	if err := s.requireGitRepo(); err != nil {
		return nil, err
	}
	return s.git.GetStatus(ctx)
}

// Diff returns the uncommitted repo diff.
func (s *Service) Diff(ctx context.Context) (string, error) {
	if err := s.requireGitRepo(); err != nil {
		return "", err
	}
	return s.git.Diff(ctx)
}
