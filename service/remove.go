package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.fuzzyporpoise.dev/lnk/v2/internal/filemanager"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
)

// Remove stops managing a path and restores it to the current machine.
func (s *Service) Remove(ctx context.Context, host, input string) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}

	// Explicitly don't normalize host here to allow for scope lookup
	resolvedHost, file, err := s.resolveRemovalScope(input, host)
	if err != nil {
		return err
	}

	fm, err := s.fileManager(resolvedHost)
	if err != nil {
		return err
	}

	removeResult, err := fm.Remove(filemanager.FileToTrack{
		AbsPath:      file.AbsPath,
		RelativePath: file.RelativePath,
	})
	if err != nil {
		return err
	}

	if err := s.git.RmCached(ctx, removeResult.RemovePaths, removeResult.IsDir); err != nil {
		return err
	}
	if err := s.stagePaths(ctx, removeResult.StagePaths...); err != nil {
		return err
	}
	if err := s.commit(ctx, fmt.Sprintf("lnk: removed from %s\n%s", host, input)); err != nil {
		return err
	}

	return removeResult.RestoreFn()
}

// Forget stops managing a path but preserves the stored repo copy.
func (s *Service) Forget(ctx context.Context, host, input string) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}

	// Explicitly don't normalize host here to allow for scope lookup
	resolvedHost, file, err := s.resolveRemovalScope(input, host)
	if err != nil {
		return err
	}

	format, err := s.getFormat()
	if err != nil {
		return err
	}
	tr := tracker.New(s.repoPath, resolvedHost, format)
	items, err := tr.GetManagedItems()
	if err != nil {
		return fmt.Errorf("read tracked items: %w", err)
	}
	if !slices.Contains(items, file.RelativePath) {
		return lnkerror.WithPath(lnkerror.ErrNotManaged, file.RelativePath)
	}

	hostPath, err := tr.HostStoragePath()
	if err != nil {
		return err
	}
	repoItem := filepath.Join(hostPath, file.RelativePath)
	if isManagedSymlink(file.AbsPath, repoItem) {
		if err := os.Remove(file.AbsPath); err != nil {
			return fmt.Errorf("remove managed symlink: %w", err)
		}
	}

	if err := tr.RemoveManagedItem(file.RelativePath); err != nil {
		return fmt.Errorf("update tracked items: %w", err)
	}
	lnkFileName, err := tr.LnkFileName()
	if err != nil {
		return err
	}
	if err := s.stagePaths(ctx, lnkFileName); err != nil {
		return err
	}
	return s.commit(ctx, fmt.Sprintf("lnk: forgot %s", filepath.Base(file.RelativePath)))
}
