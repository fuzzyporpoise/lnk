package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	fspkg "go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
)

// Move transfers ownership of a tracked path between scopes.
func (s *Service) Move(ctx context.Context, input string, toHost string, toCommon bool) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}
	file, err := s.homeRelativePath(input)
	if err != nil {
		return err
	}
	if (toCommon && toHost != "") || (!toCommon && toHost == "") {
		return lnkerror.Wrap(lnkerror.ErrInvalidFlags)
	}

	owner, err := s.findOwner(file.RelativePath)
	if err != nil {
		return err
	}
	if owner == nil {
		return lnkerror.WithPath(lnkerror.ErrNotManaged, file.RelativePath)
	}

	var targetHost string
	if toCommon {
		targetHost = tracker.CommonScope
	} else {
		targetHost = toHost
	}

	if owner.Host == targetHost {
		return lnkerror.WithPathAndSuggestion(lnkerror.ErrAlreadyManaged, file.RelativePath, fmt.Sprintf("already managed in scope %s", owner.Host))
	}

	otherOwner, err := s.findOwnerInScope(file.RelativePath, targetHost)
	if err != nil {
		return err
	}
	if otherOwner != nil {
		return lnkerror.WithPath(lnkerror.ErrAlreadyManaged, file.RelativePath)
	}

	format, err := s.getFormat()
	if err != nil {
		return err
	}
	sourceTracker := tracker.New(s.repoPath, owner.Host, format)
	targetTracker := tracker.New(s.repoPath, targetHost, format)

	srcHostPath, err := sourceTracker.HostStoragePath()
	if err != nil {
		return err
	}
	dstHostPath, err := targetTracker.HostStoragePath()
	if err != nil {
		return err
	}
	sourcePath := filepath.Join(srcHostPath, file.RelativePath)
	targetPath := filepath.Join(dstHostPath, file.RelativePath)

	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source storage path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create target storage directory: %w", err)
	}

	fs := &fspkg.FileSystem{}
	if err := fs.Move(sourcePath, targetPath, info); err != nil {
		return err
	}

	if err := sourceTracker.RemoveManagedItem(file.RelativePath); err != nil {
		_ = fs.Move(targetPath, sourcePath, info)
		return fmt.Errorf("remove source tracked item: %w", err)
	}
	if err := targetTracker.AddManagedItem(file.RelativePath); err != nil {
		_ = sourceTracker.AddManagedItem(file.RelativePath)
		_ = fs.Move(targetPath, sourcePath, info)
		return fmt.Errorf("add target tracked item: %w", err)
	}

	if err := s.repointManagedSymlink(file.RelativePath, targetPath); err != nil {
		return err
	}

	srclnkName, err := sourceTracker.LnkFileName()
	if err != nil {
		return err
	}
	tgtlnkName, err := targetTracker.LnkFileName()
	if err != nil {
		return err
	}
	if err := s.stagePaths(ctx, srclnkName, tgtlnkName, sourcePath, targetPath); err != nil {
		return err
	}
	return s.commitAuto(ctx, fmt.Sprintf("lnk: moved %s to %s", filepath.Base(file.RelativePath), targetHost))
}
