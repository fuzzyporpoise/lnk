package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// scanProjectIssues runs project-scope health checks and returns any findings.
func (s *Service) scanProjectIssues(ctx context.Context) ([]ProjectIssue, error) {
	var issues []ProjectIssue

	// Cache issues are reported first; orphaned storage skips IDs already
	// covered by a cache issue to avoid duplicate warnings.
	cacheIssues, err := s.findCacheIssues(ctx)
	if err != nil {
		return nil, fmt.Errorf("check project cache: %w", err)
	}
	issues = append(issues, cacheIssues...)
	cacheIDs := make(map[string]struct{})
	for _, issue := range cacheIssues {
		cacheIDs[issue.ProjectID] = struct{}{}
	}

	orphaned, err := s.findOrphanedProjectStorage(ctx)
	if err != nil {
		return nil, fmt.Errorf("find orphaned project storage: %w", err)
	}
	for _, issue := range orphaned {
		if _, ok := cacheIDs[issue.ProjectID]; ok {
			continue
		}
		issues = append(issues, issue)
	}

	broken, err := s.findBrokenProjectSymlinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("find broken project symlinks: %w", err)
	}
	issues = append(issues, broken...)

	return issues, nil
}

// findCacheIssues validates the machine-local project registry and returns
// warnings for entries that point to missing or mismatched checkouts, as well
// as stored projects with no registry entry at all.
func (s *Service) findCacheIssues(ctx context.Context) ([]ProjectIssue, error) {
	ps := NewProjectService(s)
	check, err := ps.CheckProjectCache(ctx)
	if err != nil {
		return nil, err
	}

	var issues []ProjectIssue
	for _, entry := range check.Missing {
		issues = append(issues, ProjectIssue{
			ProjectID:  entry.ID,
			Issue:      "cached checkout path is missing or no longer matches the project origin",
			Severity:   "warning",
			Suggestion: "run 'lnk project cache --scan <dir>' to rediscover the checkout",
		})
	}
	for _, id := range check.Uncached {
		issues = append(issues, ProjectIssue{
			ProjectID:  id,
			Issue:      "no local checkout recorded in the project registry",
			Severity:   "warning",
			Suggestion: "run 'lnk project cache --scan <dir>' to discover this project",
		})
	}

	slices.SortFunc(issues, func(a, b ProjectIssue) int {
		return strings.Compare(a.ProjectID, b.ProjectID)
	})
	return issues, nil
}

// findOrphanedProjectStorage returns issues for stored projects that have no
// available local checkout recorded in the project registry. Intentionally
// not-downloaded projects are not reported as orphaned.
func (s *Service) findOrphanedProjectStorage(ctx context.Context) ([]ProjectIssue, error) {
	stored, err := s.storedProjectIDs()
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return nil, nil
	}

	ps := NewProjectService(s)
	check, err := ps.CheckProjectCache(ctx)
	if err != nil {
		return nil, err
	}

	available := make(map[string]struct{})
	for _, entry := range check.Available {
		available[entry.ID] = struct{}{}
	}
	notDownloaded := make(map[string]struct{})
	for _, entry := range check.NotDownloaded {
		notDownloaded[entry.ID] = struct{}{}
	}

	var issues []ProjectIssue
	for _, id := range stored {
		if _, ok := available[id]; ok {
			continue
		}
		if _, ok := notDownloaded[id]; ok {
			continue
		}
		issues = append(issues, ProjectIssue{
			ProjectID: id,
			Issue:     "orphaned project storage with no available local checkout",
			Severity:  "warning",
			Suggestion: fmt.Sprintf(
				"run 'lnk project cache --scan <dir>' to rediscover, or 'rm -rf %s' if the project is no longer needed",
				filepath.Join(s.repoPath, "projects", id)),
		})
	}

	slices.SortFunc(issues, func(a, b ProjectIssue) int {
		return strings.Compare(a.ProjectID, b.ProjectID)
	})
	return issues, nil
}

// storedProjectIDs returns the IDs of all projects stored under projects/.
func (s *Service) storedProjectIDs() ([]string, error) {
	root := filepath.Join(s.repoPath, "projects")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	ids := make(map[string]struct{})
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != projectMarkerFile {
			return nil
		}
		id, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		ids[filepath.ToSlash(id)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	slices.Sort(result)
	return result, nil
}

// findBrokenProjectSymlinks walks the available project checkouts recorded in
// the project registry and reports project-scope symlinks whose storage target
// no longer exists.
func (s *Service) findBrokenProjectSymlinks(ctx context.Context) ([]ProjectIssue, error) {
	ps := NewProjectService(s)
	check, err := ps.CheckProjectCache(ctx)
	if err != nil {
		return nil, err
	}

	var issues []ProjectIssue
	for _, entry := range check.Available {
		id := entry.ID
		path := entry.Path
		storageDir := filepath.Join(s.repoPath, "projects", id)

		_ = filepath.Walk(path, func(livePath string, liveInfo os.FileInfo, err error) error {
			if err != nil || liveInfo.IsDir() {
				return nil
			}
			if liveInfo.Mode()&os.ModeSymlink == 0 {
				return nil
			}
			if !isStorageSymlink(livePath, storageDir) {
				return nil
			}

			target, err := os.Readlink(livePath)
			if err != nil {
				return nil
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(livePath), target)
			}
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				return nil
			}

			rel, err := filepath.Rel(path, livePath)
			if err != nil {
				return nil
			}

			issues = append(issues, ProjectIssue{
				ProjectID:  id,
				Issue:      fmt.Sprintf("broken symlink: %s", filepath.ToSlash(rel)),
				Severity:   "error",
				Suggestion: "run 'lnk project restore' from the project directory",
			})
			return nil
		})
	}

	slices.SortFunc(issues, func(a, b ProjectIssue) int {
		if cmp := strings.Compare(a.ProjectID, b.ProjectID); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Issue, b.Issue)
	})
	return issues, nil
}
