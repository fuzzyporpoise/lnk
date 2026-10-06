package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	fspkg "go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
)

// commonPath describes both legacy and v2 paths for commonScope
type commonPath struct {
	v1 string
	v2 string
}

// Format migrates the repo to the requested format version or reports the current format.
func (s *Service) Format(ctx context.Context, ver1, ver2 bool) (string, error) {
	if err := s.requireGitRepo(); err != nil {
		return "", err
	}
	if ver1 && ver2 {
		return "", lnkerror.Wrap(lnkerror.ErrInvalidFlags)
	}

	repoVer, err := s.FindVersion()
	if err != nil {
		return "", err
	}
	if !ver1 && !ver2 {
		return fmt.Sprintf("lnk repo format: v%d", repoVer), nil
	}
	if (ver1 && repoVer == 1) || (ver2 && repoVer == 2) {
		return fmt.Sprintf("lnk repo format already: v%d", repoVer), nil
	}

	versionItems, changedPaths, err := s.versionCommonItems()
	if err != nil {
		return "", err
	}

	if ver1 {
		err = s.migrateToV1(versionItems)
		if err == nil {
			s.format = tracker.FormatV1
		}
	} else {
		err = s.migrateToV2(versionItems)
		if err == nil {
			s.format = tracker.FormatV2
		}
	}
	if err != nil {
		return "", err
	}

	if err := fspkg.RemoveEmptyDirs(s.repoPath); err != nil {
		return "", err
	}
	if err := s.stagePaths(ctx, changedPaths...); err != nil {
		return "", err
	}
	if err := s.commitAuto(ctx, "lnk: update repo format"); err != nil {
		return "", err
	}
	return "lnk repo formatted, run `lnk doctor --fix` to fix broken symlinks", nil
}

// versionCommonItems returns the v1 and v2 storage paths for each common tracked item.
func (s *Service) versionCommonItems() (map[string]commonPath, []string, error) {
	paths := []string{".lnk", ".lnk.common", repoMarkerFile}

	format, err := s.getFormat()
	if err != nil {
		return nil, nil, err
	}
	commonItems, err := tracker.New(s.repoPath, tracker.CommonScope, format).GetManagedItems()
	if err != nil {
		return nil, nil, err
	}

	versionItems := make(map[string]commonPath, len(commonItems))
	for _, item := range commonItems {
		versionItems[item] = commonPath{
			v1: filepath.Join(s.repoPath, item),
			v2: filepath.Join(s.repoPath, "common.lnk", item),
		}
		paths = append(paths, item, filepath.Join("common.lnk", item))
	}
	return versionItems, paths, nil
}

// migrateToV1 moves common items from the v2 common.lnk directory back to the repo root.
func (s *Service) migrateToV1(versionItems map[string]commonPath) error {
	commonV1 := filepath.Join(s.repoPath, ".lnk")
	commonV2 := filepath.Join(s.repoPath, ".lnk.common")

	if err := os.Rename(commonV2, commonV1); err != nil {
		return err
	}
	for _, item := range versionItems {
		if err := os.MkdirAll(filepath.Dir(item.v1), 0o755); err != nil {
			return err
		}
		if err := os.Rename(item.v2, item.v1); err != nil {
			return err
		}
	}
	return s.writeMarkerFile(repoMarkerLegacy)
}

// migrateToV2 moves common items from the repo root into the common.lnk directory.
func (s *Service) migrateToV2(versionItems map[string]commonPath) error {
	commonV1 := filepath.Join(s.repoPath, ".lnk")
	commonV2 := filepath.Join(s.repoPath, ".lnk.common")

	if err := os.Rename(commonV1, commonV2); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.repoPath, "common.lnk"), 0o755); err != nil {
		return err
	}
	for _, item := range versionItems {
		if err := os.MkdirAll(filepath.Dir(item.v2), 0o755); err != nil {
			return err
		}
		if err := os.Rename(item.v1, item.v2); err != nil {
			return err
		}
	}
	return s.writeMarkerFile(repoMarkerVersion)
}

// FindVersion detects the repo format from the marker file or falls back to file presence.
func (s *Service) FindVersion() (tracker.RepoFormat, error) {
	// attempt to get version from marker file
	markerPath := filepath.Join(s.repoPath, repoMarkerFile)
	bytes, err := os.ReadFile(markerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// fallback to lnk profiles
			_, err = os.Stat(filepath.Join(s.repoPath, ".lnk"))
			if err == nil {
				return tracker.FormatV1, nil
			}
			return tracker.FormatUnknown, fmt.Errorf("version marker missing and unable to detect format version, run 'lnk doctor'")
		}
		return tracker.FormatUnknown, fmt.Errorf("unable to read repo marker: %w", err)
	}
	ver, ok := strings.CutPrefix(string(bytes), "version=")
	if ok {
		if n, err := strconv.Atoi(strings.TrimSpace(ver)); err == nil {
			return tracker.RepoFormat(n), nil
		}
	}

	return tracker.FormatUnknown, fmt.Errorf("repo marker version is malformed")
}

// getFormat returns the cached repo format, or reads and caches it from disk.
func (s *Service) getFormat() (tracker.RepoFormat, error) {
	if s.format != tracker.FormatUnknown {
		return s.format, nil
	}
	ver, err := s.FindVersion()
	if err != nil {
		return tracker.FormatUnknown, err
	}
	s.format = ver
	return s.format, nil
}
