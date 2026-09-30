package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/resolver"
)

// registryFile is the basename of the machine-local registry that maps stored
// project IDs to their local checkout paths. It lives outside the lnk repo so
// moving or renaming the repo does not orphan the mapping.
const registryFile = "registry.json"

// ProjectCacheState describes the local availability of a cached project.
type ProjectCacheState string

const (
	// CacheStateAvailable means the project has a valid local checkout.
	CacheStateAvailable ProjectCacheState = "available"
	// CacheStateNotDownloaded means the project is stored but intentionally
	// not present on this machine.
	CacheStateNotDownloaded ProjectCacheState = "not-downloaded"
	// CacheStateMissing means the cached path no longer points to a valid
	// checkout with a matching origin.
	CacheStateMissing ProjectCacheState = "missing"
)

// ProjectCacheEntry maps one stored project ID to a local checkout path and
// its current availability state.
type ProjectCacheEntry struct {
	ID    string            `json:"id"`
	Path  string            `json:"path"`
	State ProjectCacheState `json:"state"`
}

// ProjectCache is the on-disk cache format.
type ProjectCache struct {
	Projects []ProjectCacheEntry `json:"projects"`
}

// RegistryPath returns the machine-local registry file path:
// $LNK_REGISTRY if set, else $XDG_CACHE_HOME/lnk/registry.json, else
// ~/.cache/lnk/registry.json. If the home directory cannot be determined it
// falls back to the OS temporary directory so the registry is never written
// to an arbitrary working directory.
func RegistryPath() string {
	if override := os.Getenv("LNK_REGISTRY"); override != "" {
		return override
	}
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		return filepath.Join(cacheHome, "lnk", registryFile)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "lnk-registry.json")
	}
	return filepath.Join(home, ".cache", "lnk", registryFile)
}

// LoadProjectCache reads the local project cache. A missing cache is treated
// as an empty cache rather than an error.
func (ps *ProjectService) LoadProjectCache() (*ProjectCache, error) {
	path := RegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ProjectCache{}, nil
		}
		return nil, fmt.Errorf("read project cache: %w", err)
	}

	var cache ProjectCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse project cache: %w", err)
	}
	return &cache, nil
}

// SaveProjectCache writes the registry to disk atomically, creating the cache
// directory when it does not yet exist.
func (ps *ProjectService) SaveProjectCache(cache *ProjectCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project cache: %w", err)
	}
	data = append(data, '\n')

	path := RegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create project cache dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write project cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write project cache: %w", err)
	}
	return nil
}

// Get returns the cache entry for id, if present.
func (c *ProjectCache) Get(id string) (ProjectCacheEntry, bool) {
	for _, e := range c.Projects {
		if e.ID == id {
			return e, true
		}
	}
	return ProjectCacheEntry{}, false
}

// Set adds or updates the entry for id.
func (c *ProjectCache) Set(entry ProjectCacheEntry) {
	for i, e := range c.Projects {
		if e.ID == entry.ID {
			c.Projects[i] = entry
			return
		}
	}
	c.Projects = append(c.Projects, entry)
}

// Remove drops the entry for id.
func (c *ProjectCache) Remove(id string) {
	c.Projects = slices.DeleteFunc(c.Projects, func(e ProjectCacheEntry) bool {
		return e.ID == id
	})
}

// ProjectCacheDiscoverResult reports what a cache discovery pass changed.
type ProjectCacheDiscoverResult struct {
	Discovered []string
	Validated  []string
	Missing    []string
	Removed    []string
}

// ProjectCacheDiscover scans scanRoots for git checkouts, validates any
// existing cache entries, and updates the cache on disk. scanRoots must be
// non-empty; callers must explicitly choose which directories to scan.
// Existing entries whose cached path is invalid are marked missing; newly
// discovered projects are added as available.
func (ps *ProjectService) ProjectCacheDiscover(ctx context.Context, scanRoots []string) (ProjectCacheDiscoverResult, error) {
	if len(scanRoots) == 0 {
		return ProjectCacheDiscoverResult{}, lnkerror.WithSuggestion(lnkerror.ErrInvalidFlags, "pass at least one --scan directory")
	}

	cache, err := ps.LoadProjectCache()
	if err != nil {
		return ProjectCacheDiscoverResult{}, err
	}

	discovered, err := ps.discoverProjectRoots(ctx, scanRoots)
	if err != nil {
		return ProjectCacheDiscoverResult{}, err
	}

	stored, err := ps.ProjectListProjects()
	if err != nil {
		return ProjectCacheDiscoverResult{}, err
	}
	storedIDs := make(map[string]struct{})
	for _, p := range stored {
		storedIDs[p.ID] = struct{}{}
	}

	result := ProjectCacheDiscoverResult{}

	// Validate existing entries and mark missing ones.
	for _, entry := range cache.Projects {
		if _, ok := storedIDs[entry.ID]; !ok {
			cache.Remove(entry.ID)
			result.Removed = append(result.Removed, entry.ID)
			continue
		}

		if entry.State == CacheStateNotDownloaded {
			continue
		}

		valid, err := ps.isValidProjectRoot(ctx, entry.Path, entry.ID)
		if err != nil {
			return result, err
		}
		if valid {
			cache.Set(ProjectCacheEntry{ID: entry.ID, Path: entry.Path, State: CacheStateAvailable})
			result.Validated = append(result.Validated, entry.ID)
		} else {
			cache.Set(ProjectCacheEntry{ID: entry.ID, Path: entry.Path, State: CacheStateMissing})
			result.Missing = append(result.Missing, entry.ID)
		}
	}

	// Add newly discovered projects that are not already cached.
	for _, p := range stored {
		if _, ok := cache.Get(p.ID); ok {
			continue
		}
		candidates, ok := discovered[p.ID]
		if !ok || len(candidates) == 0 {
			continue
		}
		cache.Set(ProjectCacheEntry{ID: p.ID, Path: candidates[0], State: CacheStateAvailable})
		result.Discovered = append(result.Discovered, p.ID)
	}

	// Mark any remaining stored project without a cache entry as missing
	// so sync --all reports it clearly.
	for _, p := range stored {
		if _, ok := cache.Get(p.ID); ok {
			continue
		}
		cache.Set(ProjectCacheEntry{ID: p.ID, Path: "", State: CacheStateMissing})
	}

	slices.Sort(result.Discovered)
	slices.Sort(result.Validated)
	slices.Sort(result.Missing)
	slices.Sort(result.Removed)

	if err := ps.SaveProjectCache(cache); err != nil {
		return result, err
	}
	return result, nil
}

// isValidProjectRoot reports whether root is a git working tree whose origin
// remote normalizes to the expected project ID.
func (ps *ProjectService) isValidProjectRoot(ctx context.Context, root, expectedID string) (bool, error) {
	if root == "" {
		return false, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("check project root %s: %w", root, err)
	}
	if !info.IsDir() {
		return false, nil
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		if errors.Is(err, resolver.ErrNoOrigin) {
			return false, nil
		}
		return false, err
	}
	return id == expectedID, nil
}

// ProjectCacheCheckResult reports the health of the local project cache.
type ProjectCacheCheckResult struct {
	Available     []ProjectCacheEntry
	NotDownloaded []ProjectCacheEntry
	Missing       []ProjectCacheEntry
	Uncached      []string // stored project IDs with no cache entry
}

// recordProjectCache marks the project rooted at root with the given ID as
// available in the local cache. It is called automatically after a successful
// ProjectPush or ProjectSync.
func (ps *ProjectService) recordProjectCache(ctx context.Context, root, id string) error {
	valid, err := ps.isValidProjectRoot(ctx, root, id)
	if err != nil {
		return err
	}
	if !valid {
		return nil
	}
	cache, err := ps.LoadProjectCache()
	if err != nil {
		return err
	}
	cache.Set(ProjectCacheEntry{ID: id, Path: root, State: CacheStateAvailable})
	return ps.SaveProjectCache(cache)
}

// CheckProjectCache validates the existing cache against stored projects and
// on-disk state without modifying it.
func (ps *ProjectService) CheckProjectCache(ctx context.Context) (ProjectCacheCheckResult, error) {
	cache, err := ps.LoadProjectCache()
	if err != nil {
		return ProjectCacheCheckResult{}, err
	}

	stored, err := ps.ProjectListProjects()
	if err != nil {
		return ProjectCacheCheckResult{}, err
	}

	result := ProjectCacheCheckResult{}
	seen := make(map[string]struct{})
	for _, entry := range cache.Projects {
		seen[entry.ID] = struct{}{}
		switch entry.State {
		case CacheStateAvailable:
			valid, err := ps.isValidProjectRoot(ctx, entry.Path, entry.ID)
			if err != nil {
				return result, err
			}
			if valid {
				result.Available = append(result.Available, entry)
			} else {
				result.Missing = append(result.Missing, entry)
			}
		case CacheStateNotDownloaded:
			result.NotDownloaded = append(result.NotDownloaded, entry)
		case CacheStateMissing:
			result.Missing = append(result.Missing, entry)
		}
	}

	for _, p := range stored {
		if _, ok := seen[p.ID]; !ok {
			result.Uncached = append(result.Uncached, p.ID)
		}
	}

	slices.SortFunc(result.Available, func(a, b ProjectCacheEntry) int {
		return strings.Compare(a.ID, b.ID)
	})
	slices.SortFunc(result.NotDownloaded, func(a, b ProjectCacheEntry) int {
		return strings.Compare(a.ID, b.ID)
	})
	slices.SortFunc(result.Missing, func(a, b ProjectCacheEntry) int {
		return strings.Compare(a.ID, b.ID)
	})
	slices.Sort(result.Uncached)

	return result, nil
}
