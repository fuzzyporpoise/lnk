package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
	"go.fuzzyporpoise.dev/lnk/v2/internal/resolver"
	"go.fuzzyporpoise.dev/lnk/v2/internal/scope"
)

// DetectProjectScope checks if dir (or the current working directory when dir
// is empty) is inside a git repository that contains a .lnkinclude file. When
// a project scope is detected, it returns the project root, project ID, and a
// resolver that maps between live project paths and lnk storage paths. If dir
// is not inside a project with .lnkinclude, ok is false and err is nil. The
// lnk repository itself is never treated as a project.
func (s *Service) DetectProjectScope(ctx context.Context, dir string) (root, id string, resolver scope.Resolver, ok bool, err error) {
	if dir == "" {
		var wdErr error
		dir, wdErr = os.Getwd()
		if wdErr != nil {
			return "", "", nil, false, fmt.Errorf("get working directory: %w", wdErr)
		}
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", nil, false, fmt.Errorf("resolve path %s: %w", dir, err)
	}

	inside, gitRoot, err := gitboundary.IsInsideGitRepo(ctx, absDir)
	if err != nil {
		return "", "", nil, false, err
	}
	if !inside {
		return "", "", nil, false, nil
	}

	if s.isLnkRepoRoot(gitRoot) {
		return "", "", nil, false, nil
	}

	manifest := filepath.Join(gitRoot, ".lnkinclude")
	if _, statErr := os.Stat(manifest); errors.Is(statErr, os.ErrNotExist) {
		return "", "", nil, false, nil
	} else if statErr != nil {
		return "", "", nil, false, fmt.Errorf("check .lnkinclude: %w", statErr)
	}

	id, err = resolveProjectID(ctx, gitRoot)
	if err != nil {
		return "", "", nil, false, fmt.Errorf("resolve project id: %w", err)
	}

	storageDir := filepath.Join(s.repoPath, "projects", id)
	resolver = &scope.ProjectRootResolver{
		GitRoot:    gitRoot,
		StorageDir: storageDir,
	}

	return gitRoot, id, resolver, true, nil
}

// resolveProjectID returns the stable storage identifier for the git repo
// rooted at root. Repositories without an origin remote fall back to a local
// path-derived identifier.
func resolveProjectID(ctx context.Context, root string) (string, error) {
	id, err := resolver.ResolveProjectID(ctx, root)
	if errors.Is(err, resolver.ErrNoOrigin) {
		return resolver.LocalProjectID(root), nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// RestoreWithProject restores the effective machine profile and, when
// noProject is false, automatically detects and restores any project scope
// for the current working directory.
func (s *Service) RestoreWithProject(ctx context.Context, host string, noProject, dryRun bool) (RestoreInfo, error) {
	info, err := s.Restore(ctx, host, dryRun)
	if err != nil {
		return RestoreInfo{}, err
	}

	if noProject {
		return info, nil
	}

	root, id, _, ok, err := s.DetectProjectScope(ctx, "")
	if err != nil {
		return RestoreInfo{}, err
	}
	if !ok {
		return info, nil
	}

	fmt.Fprintf(os.Stderr, "(project scope: %s)\n", id)

	projectInfo, err := NewProjectService(s).ProjectRestore(ctx, root, dryRun, false)
	if err != nil {
		return RestoreInfo{}, err
	}

	info.Restored = append(info.Restored, projectInfo.Restored...)
	info.BackedUp = append(info.BackedUp, projectInfo.BackedUp...)
	info.SkippedTracked = append(info.SkippedTracked, projectInfo.SkippedTracked...)
	info.SkippedUnmatched = append(info.SkippedUnmatched, projectInfo.SkippedUnmatched...)

	return info, nil
}

// UpdateWithProject pulls repo changes and then restores the effective machine
// profile, including any detected project scope unless noProject is true.
func (s *Service) UpdateWithProject(ctx context.Context, host string, noProject bool) (RestoreInfo, error) {
	if err := s.Pull(ctx); err != nil {
		return RestoreInfo{}, err
	}
	return s.RestoreWithProject(ctx, host, noProject, false)
}
