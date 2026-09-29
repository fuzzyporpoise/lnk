package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
)

// List returns tracked items for common, one host, or all storage scopes.
func (s *Service) List(ctx context.Context, host string, all bool) (ListResult, error) {
	if err := s.requireGitRepo(); err != nil {
		return ListResult{}, err
	}
	if all && host != "" {
		return ListResult{}, lnkerror.Wrap(lnkerror.ErrInvalidFlags)
	}

	var scopes []string
	var err error

	if all {
		scopes, err = s.hosts()
		if err != nil {
			return ListResult{}, err
		}
	} else {
		host = NormalizeHost(host)
		scopes = []string{host}
	}

	format, err := s.getFormat()
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Scopes: make([]ScopeList, 0, len(scopes))}
	for _, scope := range scopes {
		items, err := tracker.New(s.repoPath, scope, format).GetManagedItems()
		if err != nil {
			return ListResult{}, err
		}
		active := scope == tracker.CommonScope
		if !active && len(items) > 0 {
			active = s.isHostActive(scope, items)
		}
		result.Scopes = append(result.Scopes, ScopeList{
			Name:   scope,
			Items:  items,
			Active: active,
		})
	}
	return result, nil
}

// isHostActive reports whether at least one symlink for the given host scope
// exists on the current machine and points into the repo's host storage.
func (s *Service) isHostActive(host string, items []string) bool {
	format, err := s.getFormat()
	if err != nil {
		return false
	}
	tr := tracker.New(s.repoPath, host, format)
	storagePath, err := tr.HostStoragePath()
	if err != nil {
		return false
	}
	for _, item := range items {
		livePath, err := s.resolver.ToLive(item)
		if err != nil {
			continue
		}
		target, err := os.Readlink(livePath)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(livePath), target)
		}
		target = filepath.Clean(target)
		storagePath = filepath.Clean(storagePath)
		if strings.HasPrefix(target, storagePath+string(filepath.Separator)) || target == storagePath {
			return true
		}
	}
	return false
}
