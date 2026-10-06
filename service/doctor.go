package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	gitpkg "go.fuzzyporpoise.dev/lnk/v2/internal/git"
	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
)

// ScopeResult captures doctor findings for one profile or storage scope.
type ScopeResult struct {
	Name  string
	Label string
	Items []string
}

// ResultName returns the display name for this result.
func (r ScopeResult) ResultName() string { return r.Name }

// HasIssues reports whether any items were found.
func (r ScopeResult) HasIssues() bool { return len(r.Items) > 0 }

// Print writes a human-readable summary of items to w.
func (r ScopeResult) Print(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s:\n", r.Name); err != nil {
		return err
	}
	if len(r.Items) == 0 {
		_, err := fmt.Fprintln(w, "  ok")
		return err
	}
	for _, item := range r.Items {
		if _, err := fmt.Fprintf(w, "  %s: %s\n", r.Label, item); err != nil {
			return err
		}
	}
	return nil
}

// DoctorReport captures read-only or fix-mode doctor results.
type DoctorReport struct {
	Mode                      string
	ScopeResults              []ScopeResult
	Collisions                []OwnershipCollision
	MarkerMissing             bool
	MarkerFixed               bool
	BrokenSymlinkFixSkipped   bool
	BrokenSymlinkFix          bool
	EmptyScopes               []string // host scopes with no tracked items (scan mode)
	PrunedScopes              []string // host scopes removed by --prune-empty --fix
	Projects                  []ProjectHealth
	UnmarkedProjects          []string // storage under projects/ without a marker
	EmptyProjects             []string // marked projects with no stored files
	PrunedProjects            []string // empty project storage removed by --fix --prune-empty
	ProjectIssues             []ProjectIssue
	LegacyCommitIdentity      bool // local git config carries lnk's own identity
	LegacyCommitIdentityFixed bool // legacy identity removed by --fix
}

// ProjectIssue captures a project-scope health finding for doctor.
type ProjectIssue struct {
	ProjectID  string
	Issue      string
	Severity   string // "error" or "warning"
	Suggestion string
}

// HasIssues reports whether the doctor found actionable issues.
func (r DoctorReport) HasIssues() bool {
	if r.MarkerMissing || r.LegacyCommitIdentity || len(r.Collisions) > 0 || len(r.EmptyScopes) > 0 {
		return true
	}
	if len(r.UnmarkedProjects) > 0 || len(r.EmptyProjects) > 0 || len(r.ProjectIssues) > 0 {
		return true
	}
	for _, result := range r.ScopeResults {
		if result.HasIssues() {
			return true
		}
	}
	return false
}

// Doctor scans or repairs repo and profile state.
func (s *Service) Doctor(ctx context.Context, host string, all, fix, pruneEmpty bool) (DoctorReport, error) {
	if err := s.requireGitRepo(); err != nil {
		return DoctorReport{}, err
	}
	if all && host != "" {
		return DoctorReport{}, lnkerror.Wrap(lnkerror.ErrInvalidFlags)
	}

	if fix {
		dirty, err := s.git.HasChanges(ctx)
		if err != nil {
			return DoctorReport{}, err
		}
		if dirty {
			return DoctorReport{}, lnkerror.WithSuggestion(lnkerror.ErrDirtyTree, "run 'lnk commit' before using --fix")
		}
	}

	host = NormalizeHost(host)
	report, err := s.doctorScan(ctx, host, all)
	if err != nil {
		return DoctorReport{}, err
	}
	if !fix {
		return report, nil
	}
	return s.doctorFix(ctx, host, all, pruneEmpty, report)
}

// doctorScan performs a read-only scan of repo health.
func (s *Service) doctorScan(ctx context.Context, host string, all bool) (DoctorReport, error) {
	report := DoctorReport{Mode: doctorMode(host, all)}

	markerPath := filepath.Join(s.repoPath, repoMarkerFile)
	if _, err := os.Stat(markerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.MarkerMissing = true
		} else {
			return DoctorReport{}, fmt.Errorf("checking marker file: %w", err)
		}
	}

	legacyIdentity, err := s.hasLegacyCommitIdentity(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	report.LegacyCommitIdentity = legacyIdentity

	collisions, err := s.scanCollisions()
	if err != nil {
		return DoctorReport{}, err
	}
	report.Collisions = collisions

	crossScope, err := s.scanCrossScope(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	if len(crossScope) > 0 {
		report.ScopeResults = append(report.ScopeResults, ScopeResult{
			Name:  "cross-scope",
			Label: "inside git repo",
			Items: crossScope,
		})
	}

	scopes, err := s.doctorScopes(host, all)
	if err != nil {
		return DoctorReport{}, err
	}
	for _, scope := range scopes {
		result, err := s.scanInvalidEntries(scope)
		if err != nil {
			return DoctorReport{}, err
		}
		report.ScopeResults = append(report.ScopeResults, ScopeResult{
			Name:  scope,
			Label: "invalid",
			Items: result,
		})
	}

	if !all {
		broken, err := s.scanBrokenSymlinks(host)
		if err != nil {
			return DoctorReport{}, err
		}
		report.ScopeResults = append(report.ScopeResults, ScopeResult{
			Name:  profileName(host),
			Label: "broken",
			Items: broken,
		})
	}

	empty, err := s.scanEmptyScopes()
	if err != nil {
		return DoctorReport{}, err
	}
	report.EmptyScopes = empty

	projects, unmarked, emptyProjects, err := s.scanProjects()
	if err != nil {
		return DoctorReport{}, err
	}
	report.Projects = projects
	report.UnmarkedProjects = unmarked
	report.EmptyProjects = emptyProjects

	projectIssues, err := s.scanProjectIssues(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	report.ProjectIssues = projectIssues

	return report, nil
}

// doctorFix applies fixes based on the report produced by doctorScan.
func (s *Service) doctorFix(ctx context.Context, host string, all, pruneEmpty bool, report DoctorReport) (DoctorReport, error) {
	var stagePaths []string

	if report.LegacyCommitIdentity {
		if err := s.git.UnsetLocalIdentity(ctx); err != nil {
			return DoctorReport{}, err
		}
		report.LegacyCommitIdentityFixed = true
	}

	if report.MarkerMissing {
		if err := s.writeMarkerFile(repoMarkerLegacy); err != nil {
			return DoctorReport{}, err
		}
		stagePaths = append(stagePaths, repoMarkerFile)
		report.MarkerFixed = true
	}

	for _, result := range report.ScopeResults {
		if result.Label != "invalid" || len(result.Items) == 0 {
			continue
		}
		scope := result.Name
		format, err := s.getFormat()
		if err != nil {
			return DoctorReport{}, err
		}
		tr := tracker.New(s.repoPath, scope, format)
		items, err := tr.GetManagedItems()
		if err != nil {
			return DoctorReport{}, err
		}
		filtered := slices.DeleteFunc(slices.Clone(items), func(item string) bool {
			return slices.Contains(result.Items, item)
		})
		if err := tr.WriteManagedItems(filtered); err != nil {
			return DoctorReport{}, err
		}
		lnkFileName, err := tr.LnkFileName()
		if err != nil {
			return DoctorReport{}, err
		}
		stagePaths = append(stagePaths, lnkFileName)
	}

	if !all {
		broken, err := s.scanBrokenSymlinks(host)
		if err != nil {
			return DoctorReport{}, err
		}
		if len(broken) > 0 {
			if _, err := s.Restore(ctx, host, false); err != nil {
				return DoctorReport{}, err
			}
			report.BrokenSymlinkFix = true
		}
	} else {
		report.BrokenSymlinkFixSkipped = true
	}

	if pruneEmpty && (host == tracker.CommonScope || all) && len(report.EmptyScopes) > 0 {
		pruned, paths, err := s.pruneEmptyScopes(report.EmptyScopes)
		if err != nil {
			return DoctorReport{}, err
		}
		report.PrunedScopes = pruned
		stagePaths = append(stagePaths, paths...)
	}

	if pruneEmpty && len(report.EmptyProjects) > 0 {
		pruned, paths, err := s.pruneEmptyProjects(report.EmptyProjects)
		if err != nil {
			return DoctorReport{}, err
		}
		report.PrunedProjects = pruned
		stagePaths = append(stagePaths, paths...)
	}

	if len(stagePaths) == 0 {
		return report, nil
	}

	slices.Sort(stagePaths)
	stagePaths = slices.Compact(stagePaths)
	if err := s.stagePaths(ctx, stagePaths...); err != nil {
		return DoctorReport{}, err
	}

	// Skip commit if staging produced no changes (e.g., file was never tracked)
	hasChanges, err := s.git.HasChanges(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	if !hasChanges {
		return report, nil
	}

	return report, s.commitAuto(ctx, "lnk: doctor fixes")
}

// hasLegacyCommitIdentity reports whether the repo's local git config carries
// the exact identity older lnk versions wrote ("Lnk User <lnk@localhost>").
// That entry shadows the user's real identity for their own commits, so doctor
// surfaces it and --fix removes it. Only the exact pair is matched, so a user
// who intentionally configured that name is not affected.
func (s *Service) hasLegacyCommitIdentity(ctx context.Context) (bool, error) {
	name, email, err := s.git.LocalIdentity(ctx)
	if err != nil {
		return false, err
	}
	return name == gitpkg.LnkCommitName && email == gitpkg.LnkCommitEmail, nil
}

// scanCrossScope finds tracked paths that are inside a git repo. These belong
// in project scope, not common/host scope.
func (s *Service) scanCrossScope(ctx context.Context) ([]string, error) {
	format, err := s.getFormat()
	if err != nil {
		return nil, err
	}

	hosts, err := s.hosts()
	if err != nil {
		return nil, err
	}

	var violations []string
	for _, host := range hosts {
		items, itemErr := tracker.New(s.repoPath, host, format).GetManagedItems()
		if itemErr != nil {
			return nil, itemErr
		}
		for _, item := range items {
			absPath, err := s.resolver.ToLive(item)
			if err != nil {
				return nil, err
			}
			if _, statErr := os.Stat(absPath); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("check cross-scope for %s: %w", item, statErr)
			}

			inside, _, err := gitboundary.IsInsideGitRepo(ctx, absPath)
			if err != nil {
				return nil, err
			}
			if inside {
				violations = append(violations, item)
			}
		}
	}

	slices.Sort(violations)
	return violations, nil
}

// scanEmptyScopes returns host scope names whose tracker files exist but
// contain no managed items.
func (s *Service) scanEmptyScopes() ([]string, error) {
	hosts, err := s.hosts()
	if err != nil {
		return nil, err
	}
	format, err := s.getFormat()
	if err != nil {
		return nil, err
	}
	var empty []string
	for _, host := range hosts {
		if host == tracker.CommonScope {
			continue
		}
		items, err := tracker.New(s.repoPath, host, format).GetManagedItems()
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			empty = append(empty, host)
		}
	}
	slices.Sort(empty)
	return empty, nil
}

// pruneEmptyScopes removes the tracker file and storage directory for each
// empty host scope. Returns the pruned scope names and the git paths to stage.
func (s *Service) pruneEmptyScopes(emptyScopes []string) ([]string, []string, error) {
	format, err := s.getFormat()
	if err != nil {
		return nil, nil, err
	}
	var pruned []string
	var stagePaths []string

	for _, host := range emptyScopes {
		tr := tracker.New(s.repoPath, host, format)

		// Remove tracker file.
		lnkFileName, err := tr.LnkFileName()
		if err != nil {
			return nil, nil, err
		}
		trackerPath := filepath.Join(s.repoPath, lnkFileName)
		if err := os.Remove(trackerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("remove tracker for %s: %w", host, err)
		}
		stagePaths = append(stagePaths, lnkFileName)

		// Remove storage directory if it exists and is empty.
		storagePath, err := tr.HostStoragePath()
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(storagePath); err == nil {
			if err := fs.RemoveEmptyDirs(storagePath); err != nil {
				return nil, nil, fmt.Errorf("remove empty storage dirs for %s: %w", host, err)
			}
			// Remove the storage root itself if now empty; ignore "not empty" errors.
			_ = os.Remove(storagePath)
		}

		pruned = append(pruned, host)
	}

	return pruned, stagePaths, nil
}

// scanInvalidEntries finds tracked items that are missing from disk or have invalid paths.
func (s *Service) scanInvalidEntries(host string) ([]string, error) {
	format, err := s.getFormat()
	if err != nil {
		return nil, err
	}
	tr := tracker.New(s.repoPath, host, format)
	items, err := tr.GetManagedItems()
	if err != nil {
		return nil, err
	}
	invalid := make([]string, 0)
	for _, item := range items {
		if !isValidRelativePath(item) {
			invalid = append(invalid, item)
			continue
		}
		storagePath, err := tr.HostStoragePath()
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(storagePath, item)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				invalid = append(invalid, item)
			} else {
				return nil, fmt.Errorf("checking repo path for %s: %w", item, err)
			}
		}
	}
	slices.Sort(invalid)
	return invalid, nil
}

// isValidRelativePath checks whether a path is safe to use as a tracked relative path.
func isValidRelativePath(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned != "." && !strings.HasPrefix(cleaned, "..") && !filepath.IsAbs(cleaned)
}

// scanBrokenSymlinks finds tracked live paths that are not symlinks pointing to the repo.
func (s *Service) scanBrokenSymlinks(host string) ([]string, error) {
	items, err := s.profileItems(host)
	if err != nil {
		return nil, err
	}
	broken := make([]string, 0)
	for _, item := range items {
		if _, err := os.Stat(item.RepoPath); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if !isManagedSymlink(item.LivePath, item.RepoPath) {
			broken = append(broken, item.RelativePath)
		}
	}
	slices.Sort(broken)
	return broken, nil
}

// profileName returns a display name for a host profile.
func profileName(host string) string {
	return "profile:" + host
}

// doctorMode returns a string describing the doctor mode for the report.
func doctorMode(host string, all bool) string {
	if all {
		return "all"
	}
	return "profile:" + host
}

// doctorScopes returns the list of scopes to inspect for the given mode.
func (s *Service) doctorScopes(host string, all bool) ([]string, error) {
	if all {
		// Service hosts includes common by default
		hosts, err := s.hosts()
		if err != nil {
			return []string{}, err
		}
		return hosts, nil
	}

	// Doctor scope should default to common even if no host is passed
	scope := []string{tracker.CommonScope}
	if host != tracker.CommonScope {
		return append(scope, host), nil
	}
	return scope, nil
}
