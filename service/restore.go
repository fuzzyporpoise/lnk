package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	fspkg "go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

// Restore applies the effective machine profile: common only, or common + host.
func (s *Service) Restore(ctx context.Context, host string, dryRun bool) (RestoreInfo, error) {
	if err := s.requireGitRepo(); err != nil {
		return RestoreInfo{}, err
	}
	collisions, err := s.scanCollisions()
	if err != nil {
		return RestoreInfo{}, err
	}
	if len(collisions) > 0 {
		return RestoreInfo{}, lnkerror.WithSuggestion(lnkerror.ErrDuplicateOwnership, "run 'lnk doctor' first")
	}

	host = NormalizeHost(host)
	items, err := s.profileItems(host)
	if err != nil {
		return RestoreInfo{}, err
	}

	info := RestoreInfo{}
	fs := &fspkg.FileSystem{}
	for _, item := range items {
		if _, err := os.Stat(item.RepoPath); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if isManagedSymlink(item.LivePath, item.RepoPath) {
			continue
		}

		if currentInfo, err := os.Lstat(item.LivePath); err == nil {
			if currentInfo.Mode()&os.ModeSymlink == 0 {
				info.BackedUp = append(info.BackedUp, item.RelativePath)
				if !dryRun {
					backupPath := item.LivePath + ".lnk-backup"
					if _, err := os.Lstat(backupPath); err == nil {
						return RestoreInfo{}, lnkerror.WithPath(lnkerror.ErrBackupExists, backupPath)
					}
					if err := os.Rename(item.LivePath, backupPath); err != nil {
						return RestoreInfo{}, fmt.Errorf("backup existing file %s: %w", item.LivePath, err)
					}
				}
			} else if !dryRun {
				if err := os.Remove(item.LivePath); err != nil {
					return RestoreInfo{}, fmt.Errorf("remove stale symlink %s: %w", item.LivePath, err)
				}
			}
		}

		info.Restored = append(info.Restored, item.RelativePath)
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(item.LivePath), 0o755); err != nil {
			return RestoreInfo{}, fmt.Errorf("create live parent directory: %w", err)
		}
		if err := fs.CreateSymlink(item.RepoPath, item.LivePath); err != nil {
			return RestoreInfo{}, err
		}
	}
	return info, nil
}

// RestoreHook is the collision-safe variant of Restore used by the
// post-merge git hook in the lnk repo. It creates missing symlinks, skips
// symlinks that already resolve correctly, replaces stale symlinks, and
// reports real-file collisions without backing them up.
func (s *Service) RestoreHook(ctx context.Context) (RestoreInfo, error) {
	if err := s.requireGitRepo(); err != nil {
		return RestoreInfo{}, err
	}

	host := NormalizeHost("")
	items, err := s.profileItems(host)
	if err != nil {
		return RestoreInfo{}, err
	}

	info := RestoreInfo{}
	fs := &fspkg.FileSystem{}
	for _, item := range items {
		if _, err := os.Stat(item.RepoPath); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if isManagedSymlink(item.LivePath, item.RepoPath) {
			continue
		}

		currentInfo, err := os.Lstat(item.LivePath)
		if err == nil {
			if currentInfo.Mode()&os.ModeSymlink == 0 {
				info.Collisions = append(info.Collisions, item.RelativePath)
				continue
			}
			if err := os.Remove(item.LivePath); err != nil {
				return RestoreInfo{}, fmt.Errorf("remove stale symlink %s: %w", item.LivePath, err)
			}
		}

		info.Restored = append(info.Restored, item.RelativePath)
		if err := os.MkdirAll(filepath.Dir(item.LivePath), 0o755); err != nil {
			return RestoreInfo{}, fmt.Errorf("create live parent directory: %w", err)
		}
		if err := fs.CreateSymlink(item.RepoPath, item.LivePath); err != nil {
			return RestoreInfo{}, err
		}
	}
	return info, nil
}
