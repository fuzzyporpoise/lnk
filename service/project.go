package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	fspkg "go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	gitpkg "go.fuzzyporpoise.dev/lnk/v2/internal/git"
	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/internal/patterns"
	"go.fuzzyporpoise.dev/lnk/v2/internal/resolver"
	"go.fuzzyporpoise.dev/lnk/v2/internal/scope"
)

// ProjectService implements project-scope operations for the lnk repo
// managed by the embedded Service.
type ProjectService struct {
	svc *Service
}

// NewProjectService creates a ProjectService backed by svc.
func NewProjectService(svc *Service) *ProjectService {
	return &ProjectService{svc: svc}
}

// projectMarkerFile marks the storage root of one project, so that project
// directories can be enumerated even though a project ID contains slashes.
const projectMarkerFile = ".lnkproject"

// projectsDirName is the repo-relative root of all project storage. Automatic
// project commits are scoped to this subtree so they never sweep in unrelated
// repo changes, which belong to the user's own commits.
const projectsDirName = "projects"

// commitProjectChanges stages the project storage tree and commits it when it
// has changes. Staging, the change check, and the commit itself are all scoped
// to projects/, so unrelated changes elsewhere in the repo, staged or not, are
// neither committed nor mistaken for project changes.
func (ps *ProjectService) commitProjectChanges(ctx context.Context, message string) error {
	if err := ps.svc.stagePaths(ctx, projectsDirName); err != nil {
		return err
	}
	hasChanges, err := ps.svc.git.HasChanges(ctx, projectsDirName)
	if err != nil {
		return err
	}
	if !hasChanges {
		return nil
	}
	return ps.svc.commitAutoIn(ctx, message, projectsDirName)
}

// ProjectInit activates project scope for the git repo containing
// projectRoot by creating an empty .lnkinclude file at the repo root if one
// does not already exist. It returns true when the file was created and
// false when it already existed.
func (ps *ProjectService) ProjectInit(ctx context.Context, projectRoot string) (bool, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return false, err
	}

	manifest := filepath.Join(root, ".lnkinclude")
	if _, err := os.Stat(manifest); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("check .lnkinclude: %w", err)
	}

	if err := os.WriteFile(manifest, []byte{}, 0o644); err != nil {
		return false, fmt.Errorf("create .lnkinclude: %w", err)
	}
	return true, nil
}

// ProjectAddPattern appends a pattern to the project's .lnkinclude file at
// the git root. Existing on-disk paths inside the project are normalized to
// project-relative form; anything else (globs, ! negations, files that do
// not exist yet) is stored verbatim. It returns the stored pattern and
// whether it matches at least one existing file (always true for negations,
// which are not match-checked).
func (ps *ProjectService) ProjectAddPattern(ctx context.Context, projectRoot, rawPattern string) (string, bool, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return "", false, err
	}

	pattern, err := normalizePattern(root, rawPattern)
	if err != nil {
		return "", false, err
	}

	manifest := filepath.Join(root, ".lnkinclude")
	existing, err := patterns.Load(manifest)
	if err != nil {
		return "", false, fmt.Errorf("load .lnkinclude: %w", err)
	}
	if slices.Contains(existing, pattern) {
		return "", false, lnkerror.WithPath(lnkerror.ErrAlreadyManaged, pattern)
	}

	matched := true
	if !strings.HasPrefix(pattern, "!") {
		matched, err = matchesAnyFile(root, pattern)
		if err != nil {
			return "", false, err
		}
	}

	if err := appendPattern(manifest, pattern); err != nil {
		return "", false, err
	}

	return pattern, matched, nil
}

// ProjectListPatterns returns the effective patterns for a project, split
// into global (lnk repo root) and local (.lnkinclude at the git root) lists.
func (ps *ProjectService) ProjectListPatterns(ctx context.Context, projectRoot string) (global, local []string, err error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return nil, nil, err
	}

	global, err = patterns.Load(filepath.Join(ps.svc.RepoPath(), ".lnkinclude"))
	if err != nil {
		return nil, nil, fmt.Errorf("load global .lnkinclude: %w", err)
	}

	local, err = patterns.Load(filepath.Join(root, ".lnkinclude"))
	if err != nil {
		return nil, nil, fmt.Errorf("load local .lnkinclude: %w", err)
	}

	return global, local, nil
}

// ProjectUntrackResult reports the outcome of ProjectUntrackPattern.
type ProjectUntrackResult struct {
	// Removed is true when the pattern was found in (and removed from) the
	// local .lnkinclude.
	Removed bool
	// IsGlobal is true when the pattern only exists in the global
	// .lnkinclude, which must be edited directly.
	IsGlobal bool
	// Released lists stored files moved back to the project because they no
	// longer match the effective patterns (empty when keep is set).
	Released []string
	// BackedUp lists live files renamed to .lnk-backup during release.
	BackedUp []string
}

// ProjectUntrackPattern removes a pattern from the project's .lnkinclude at
// the git root. Unless keep is set, stored files that no longer match the
// effective patterns are moved back to their live paths (mirroring 'lnk
// remove' for host scope) and the storage change is committed. When the
// pattern exists only in the global file, IsGlobal is set so the caller can
// print a warning.
func (ps *ProjectService) ProjectUntrackPattern(ctx context.Context, projectRoot, pattern string, keep bool) (ProjectUntrackResult, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return ProjectUntrackResult{}, err
	}

	localPath := filepath.Join(root, ".lnkinclude")
	localLines, err := patterns.Load(localPath)
	if err != nil {
		return ProjectUntrackResult{}, fmt.Errorf("load local .lnkinclude: %w", err)
	}

	// Accept the pattern as written or its normalized form, so 'untrack
	// .todo/' matches a '.todo' entry relativized at add time (and vice
	// versa for verbatim glob patterns).
	target := pattern
	if !slices.Contains(localLines, target) {
		if normalized, normErr := normalizePattern(root, pattern); normErr == nil && normalized != pattern {
			if slices.Contains(localLines, normalized) {
				target = normalized
			}
		}
	}

	if !slices.Contains(localLines, target) {
		global, err := patterns.Load(filepath.Join(ps.svc.RepoPath(), ".lnkinclude"))
		if err != nil {
			return ProjectUntrackResult{}, fmt.Errorf("load global .lnkinclude: %w", err)
		}
		if slices.Contains(global, target) {
			return ProjectUntrackResult{IsGlobal: true}, nil
		}
		return ProjectUntrackResult{}, lnkerror.WithPath(lnkerror.ErrNotManaged, pattern)
	}

	if err := rewritePatterns(localPath, localLines, target); err != nil {
		return ProjectUntrackResult{}, err
	}
	result := ProjectUntrackResult{Removed: true}

	if keep {
		return result, nil
	}

	effective, err := ps.effectivePatterns(root)
	if err != nil {
		return result, err
	}
	released, backedUp, err := ps.releaseUnmatched(ctx, root, effective, false)
	if err != nil {
		return result, err
	}
	result.Released = released
	result.BackedUp = backedUp

	if len(released) > 0 {
		id, err := ps.projectID(ctx, root)
		if err != nil {
			return result, err
		}
		if err := ps.commitProjectChanges(ctx, "lnk: untracked '"+target+"' in project "+id); err != nil {
			return result, err
		}
	}

	return result, nil
}

// effectivePatterns returns the combined global and local pattern lists.
// Local entries come last so they can negate global ones.
func (ps *ProjectService) effectivePatterns(root string) ([]string, error) {
	global, err := patterns.Load(filepath.Join(ps.svc.RepoPath(), ".lnkinclude"))
	if err != nil {
		return nil, fmt.Errorf("load global .lnkinclude: %w", err)
	}
	local, err := patterns.Load(filepath.Join(root, ".lnkinclude"))
	if err != nil {
		return nil, fmt.Errorf("load local .lnkinclude: %w", err)
	}
	return slices.Concat(global, local), nil
}

// resolveProjectRoot anchors a project command at the root of the git
// working tree containing dir, and refuses the lnk repository itself:
// treating it as a project would store the repo inside its own storage.
func (ps *ProjectService) resolveProjectRoot(ctx context.Context, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}

	root, err := gitboundary.ResolveGitRoot(ctx, abs)
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", lnkerror.WithPathAndSuggestion(lnkerror.ErrOutsideGitRepo, abs, "use 'lnk add' for host/common scope")
	}

	if ps.svc.isLnkRepoRoot(root) {
		return "", lnkerror.WithPathAndSuggestion(lnkerror.ErrIsLnkRepository, root, "the lnk repo manages itself; project scope is for other git repositories")
	}

	return root, nil
}

// projectID resolves the storage identifier for the project, falling back to
// a local path-derived identifier when the repo has no origin remote.
func (ps *ProjectService) projectID(ctx context.Context, root string) (string, error) {
	id, err := resolver.ResolveProjectID(ctx, root)
	if errors.Is(err, resolver.ErrNoOrigin) {
		return resolver.LocalProjectID(root), nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve project id: %w", err)
	}
	return id, nil
}

// normalizePattern converts raw into the pattern stored in .lnkinclude. When
// raw names an existing file or directory it is relativized to the project
// root, and existing paths outside the project are rejected. Anything else
// (glob patterns, ! negations, files that do not exist yet) is returned
// verbatim.
func normalizePattern(projectRoot, raw string) (string, error) {
	pattern := strings.TrimSpace(raw)
	body := strings.TrimPrefix(pattern, "!")
	if body == "" {
		return "", lnkerror.Wrap(lnkerror.ErrEmptyPattern)
	}

	candidate := body
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(projectRoot, candidate)
	}
	if _, err := os.Lstat(candidate); err != nil {
		return pattern, nil
	}

	// Canonicalize the directory portion of both sides: git reports the repo
	// root with symlinks resolved, while the user may pass a path through a
	// symlinked prefix. The leaf is never resolved: an already-managed file
	// is a symlink into lnk storage and must still relativize to its live
	// path inside the project.
	canonicalRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		canonicalRoot = projectRoot
	}
	canonicalDir, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		canonicalDir = filepath.Dir(candidate)
	}
	canonicalCandidate := filepath.Join(canonicalDir, filepath.Base(candidate))

	rel, err := filepath.Rel(canonicalRoot, canonicalCandidate)
	if err != nil {
		return "", fmt.Errorf("relativize pattern %s: %w", raw, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", lnkerror.WithPathAndSuggestion(lnkerror.ErrOutsideProject, raw, "patterns must match files inside the project")
	}
	if strings.HasPrefix(pattern, "!") {
		return "!" + rel, nil
	}
	return rel, nil
}

// errPatternMatched stops the walk early once matchesAnyFile finds a hit.
var errPatternMatched = errors.New("pattern matched")

// matchesAnyFile reports whether pattern matches at least one existing file
// in the project.
func matchesAnyFile(root, pattern string) (bool, error) {
	err := walkProjectFiles(root, func(_, rel string) error {
		ok, err := patterns.Match([]string{pattern}, rel)
		if err != nil {
			return err
		}
		if ok {
			return errPatternMatched
		}
		return nil
	})
	if errors.Is(err, errPatternMatched) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// walkProjectFiles calls fn for every regular, non-symlinked file under
// root, pruning .git directories and nested git working trees. The rel path
// passed to fn uses '/' separators relative to root.
func walkProjectFiles(root string, fn func(absPath, rel string) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			if path != root && hasGitMarker(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(path, filepath.ToSlash(rel))
	})
}

// hasGitMarker reports whether dir is the root of a git working tree: a
// .git directory, or a .git file as used by submodules and linked worktrees.
func hasGitMarker(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// implicitlyExcluded reports whether rel is lnk metadata that must never be
// managed, regardless of the effective patterns.
func implicitlyExcluded(rel string) bool {
	return rel == ".lnkinclude" || strings.HasSuffix(rel, ".lnk-backup")
}

// projectTrackedFiles returns the set of slash-separated, root-relative
// paths tracked by the project's own git index.
func projectTrackedFiles(ctx context.Context, root string) (map[string]struct{}, error) {
	out, err := gitpkg.New(root).Run(ctx, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("list project-tracked files: %w\n%s", err, out)
	}
	tracked := make(map[string]struct{})
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			tracked[p] = struct{}{}
		}
	}
	return tracked, nil
}

// appendPattern appends pattern to manifest, ensuring a preceding newline
// when the file already exists and does not end with one.
func appendPattern(manifest, pattern string) error {
	f, err := os.OpenFile(manifest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open .lnkinclude: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	info, err := os.Stat(manifest)
	if err == nil && info.Size() > 0 {
		data, err := os.ReadFile(manifest)
		if err == nil && len(data) > 0 && data[len(data)-1] != '\n' {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("write newline: %w", err)
			}
		}
	}

	if _, err := f.WriteString(pattern + "\n"); err != nil {
		return fmt.Errorf("write pattern: %w", err)
	}
	return nil
}

// rewritePatterns writes lines back to manifest, excluding dropPattern.
func rewritePatterns(manifest string, lines []string, dropPattern string) error {
	f, err := os.Create(manifest)
	if err != nil {
		return fmt.Errorf("rewrite .lnkinclude: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	w := bufio.NewWriter(f)
	for _, p := range lines {
		if p == dropPattern {
			continue
		}
		if _, err := w.WriteString(p + "\n"); err != nil {
			return fmt.Errorf("write pattern: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush .lnkinclude: %w", err)
	}
	return nil
}

// ProjectPushResult reports the files moved to storage and symlinked back by
// ProjectPush.
type ProjectPushResult struct {
	ProjectID string
	Synced    []string
	// SkippedTracked lists matched files left untouched because the
	// project's own git index tracks them (requires force to manage).
	SkippedTracked []string
}

// ProjectPush walks the project repository, moves matching files to the lnk
// project storage directory, and symlinks them back. It then stages and
// commits the changes in the lnk repo. Files tracked by the project's own
// git index are skipped unless force is set, since replacing them with
// symlinks would dirty the project's working tree with a typechange. Files
// that fail to move are collected and reported as an aggregate error after
// the rest are synced.
func (ps *ProjectService) ProjectPush(ctx context.Context, projectRoot string, force bool) (ProjectPushResult, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return ProjectPushResult{}, err
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		return ProjectPushResult{}, err
	}

	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return ProjectPushResult{}, fmt.Errorf("create project storage: %w", err)
	}
	if err := ensureProjectMarker(storageDir, id); err != nil {
		return ProjectPushResult{}, err
	}

	global, err := patterns.Load(filepath.Join(ps.svc.RepoPath(), ".lnkinclude"))
	if err != nil {
		return ProjectPushResult{}, fmt.Errorf("load global .lnkinclude: %w", err)
	}

	local, err := patterns.Load(filepath.Join(root, ".lnkinclude"))
	if err != nil {
		return ProjectPushResult{}, fmt.Errorf("load local .lnkinclude: %w", err)
	}

	effective := slices.Concat(global, local)
	if len(effective) == 0 {
		return ProjectPushResult{}, lnkerror.Wrap(lnkerror.ErrNoPatterns)
	}

	tracked, err := projectTrackedFiles(ctx, root)
	if err != nil {
		return ProjectPushResult{}, err
	}

	stats, err := syncNewMatches(&fspkg.FileSystem{}, root, storageDir, effective, tracked, force, false)
	if err != nil {
		return ProjectPushResult{}, err
	}

	result := ProjectPushResult{
		ProjectID:      id,
		Synced:         stats.synced,
		SkippedTracked: stats.skippedTracked,
	}

	if err := ps.commitProjectChanges(ctx, "lnk: sync project "+id); err != nil {
		return result, err
	}

	if len(stats.failed) > 0 {
		return result, fmt.Errorf("%w: %w", lnkerror.ErrSyncFailed, errors.Join(stats.failed...))
	}

	if err := ps.recordProjectCache(ctx, root, id); err != nil {
		return result, err
	}

	return result, nil
}

// moveToStorage moves livePath to storagePath and symlinks it back. If
// linking fails the move is rolled back so the live file is never lost.
func moveToStorage(fs *fspkg.FileSystem, livePath, storagePath string) error {
	if err := os.MkdirAll(filepath.Dir(storagePath), 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	if err := fs.MoveFile(livePath, storagePath); err != nil {
		return fmt.Errorf("move to storage: %w", err)
	}
	if err := fs.CreateSymlink(storagePath, livePath); err != nil {
		if rbErr := fs.MoveFile(storagePath, livePath); rbErr != nil {
			return fmt.Errorf("create symlink: %w (rollback failed: %v)", err, rbErr)
		}
		return fmt.Errorf("create symlink: %w", err)
	}
	return nil
}

// matchSyncStats accumulates the outcome of syncNewMatches.
type matchSyncStats struct {
	synced         []string
	skippedTracked []string
	failed         []error
}

// syncNewMatches moves live files matching the effective patterns into
// storage and symlinks them back (project reconciliation class 1: newly
// matched files). Files tracked by the project's own git are skipped unless
// force is set. In dryRun mode matches are reported but not moved and move
// failures are collected per file rather than aborting the walk.
func syncNewMatches(fs *fspkg.FileSystem, root, storageDir string, effective []string, tracked map[string]struct{}, force, dryRun bool) (matchSyncStats, error) {
	var stats matchSyncStats
	err := walkProjectFiles(root, func(path, rel string) error {
		if implicitlyExcluded(rel) {
			return nil
		}
		match, err := patterns.Match(effective, rel)
		if err != nil {
			return err
		}
		if !match {
			return nil
		}
		if !force {
			if _, ok := tracked[rel]; ok {
				stats.skippedTracked = append(stats.skippedTracked, rel)
				return nil
			}
		}
		if dryRun {
			stats.synced = append(stats.synced, rel)
			return nil
		}
		if err := moveToStorage(fs, path, filepath.Join(storageDir, filepath.FromSlash(rel))); err != nil {
			stats.failed = append(stats.failed, fmt.Errorf("%s: %w", rel, err))
			return nil
		}
		stats.synced = append(stats.synced, rel)
		return nil
	})
	return stats, err
}

// releaseFile moves a stored file back to its live path. A symlink pointing
// into storageDir is simply removed; any other existing live file (or a
// foreign symlink) is first renamed to .lnk-backup. It reports whether a
// backup was made.
func releaseFile(fs *fspkg.FileSystem, storagePath, livePath, storageDir string) (backedUp bool, err error) {
	if liveInfo, statErr := os.Lstat(livePath); statErr == nil {
		if liveInfo.Mode()&os.ModeSymlink != 0 && isStorageSymlink(livePath, storageDir) {
			if err := os.Remove(livePath); err != nil {
				return false, fmt.Errorf("remove managed symlink: %w", err)
			}
		} else {
			backupPath := livePath + ".lnk-backup"
			if _, err := os.Lstat(backupPath); err == nil {
				return false, lnkerror.WithPath(lnkerror.ErrBackupExists, backupPath)
			}
			if err := os.Rename(livePath, backupPath); err != nil {
				return false, fmt.Errorf("backup existing file %s: %w", livePath, err)
			}
			backedUp = true
		}
	}
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		return false, fmt.Errorf("create live parent directory: %w", err)
	}
	if err := fs.MoveFile(storagePath, livePath); err != nil {
		return false, fmt.Errorf("restore live file: %w", err)
	}
	return backedUp, nil
}

// isStorageSymlink reports whether livePath is a symlink whose target lies
// inside storageDir.
func isStorageSymlink(livePath, storageDir string) bool {
	target, err := os.Readlink(livePath)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(livePath), target)
	}
	target = filepath.Clean(target)
	rel, err := filepath.Rel(storageDir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// releaseUnmatched moves stored files that no longer match the effective
// patterns back to their live paths (project reconciliation class 2: pattern
// drift). In dryRun mode the affected paths are reported but not moved.
func (ps *ProjectService) releaseUnmatched(ctx context.Context, root string, effective []string, dryRun bool) (released, backedUp []string, err error) {
	id, err := ps.projectID(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if _, err := os.Stat(storageDir); err != nil {
		return nil, nil, nil
	}

	fs := &fspkg.FileSystem{}
	var failed []error

	err = filepath.Walk(storageDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(storageDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == projectMarkerFile {
			return nil
		}

		match, err := patterns.Match(effective, rel)
		if err != nil {
			return err
		}
		if match {
			return nil
		}

		if dryRun {
			released = append(released, rel)
			return nil
		}
		backed, err := releaseFile(fs, path, filepath.Join(root, filepath.FromSlash(rel)), storageDir)
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", rel, err))
			return nil
		}
		released = append(released, rel)
		if backed {
			backedUp = append(backedUp, rel)
		}
		return nil
	})
	if err != nil {
		return released, backedUp, err
	}

	if !dryRun {
		if err := fspkg.RemoveEmptyDirs(storageDir); err != nil {
			return released, backedUp, fmt.Errorf("prune empty storage directories: %w", err)
		}
	}

	if len(failed) > 0 {
		return released, backedUp, fmt.Errorf("%w: %w", lnkerror.ErrSyncFailed, errors.Join(failed...))
	}
	return released, backedUp, nil
}

// liveDeletions finds stored files that still match the effective patterns
// but whose live paths have gone missing (project reconciliation class 3).
// When prune is set their storage copies are deleted; otherwise they are
// only reported. In dryRun mode nothing is deleted.
func (ps *ProjectService) liveDeletions(ctx context.Context, root string, effective []string, dryRun, prune bool) (deletions, pruned []string, err error) {
	id, err := ps.projectID(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if _, err := os.Stat(storageDir); err != nil {
		return nil, nil, nil
	}

	err = filepath.Walk(storageDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(storageDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == projectMarkerFile {
			return nil
		}

		match, err := patterns.Match(effective, rel)
		if err != nil {
			return err
		}
		if !match {
			return nil
		}

		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			return nil
		}

		if prune {
			pruned = append(pruned, rel)
			if !dryRun {
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("prune stored file %s: %w", rel, err)
				}
			}
			return nil
		}
		deletions = append(deletions, rel)
		return nil
	})
	if err != nil {
		return deletions, pruned, err
	}

	if prune && !dryRun {
		if err := fspkg.RemoveEmptyDirs(storageDir); err != nil {
			return deletions, pruned, fmt.Errorf("prune empty storage directories: %w", err)
		}
	}
	return deletions, pruned, nil
}

// ProjectSyncResult reports the reconciliation outcome of ProjectSync.
type ProjectSyncResult struct {
	ProjectID string
	// Synced lists live files newly moved to storage and symlinked back.
	Synced []string
	// Released lists stored files moved back to the project because their
	// patterns no longer match.
	Released []string
	// BackedUp lists live files renamed to .lnk-backup during release.
	BackedUp []string
	// Deletions lists stored files whose live copies are gone (kept unless
	// pruneDeletions is set).
	Deletions []string
	// Pruned lists stored files deleted because their live copies are gone.
	Pruned []string
	// SkippedTracked lists matched files left untouched because the
	// project's own git index tracks them.
	SkippedTracked []string
}

// ProjectSync reconciles live files, stored files, and the effective
// patterns in both directions: newly matched files are pushed to storage,
// files whose patterns no longer match are moved back to the project, and
// stored files whose live copies were deleted are reported (or pruned with
// pruneDeletions). Storage changes are staged and committed in the lnk repo.
func (ps *ProjectService) ProjectSync(ctx context.Context, projectRoot string, dryRun, pruneDeletions, force bool) (ProjectSyncResult, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return ProjectSyncResult{}, err
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		return ProjectSyncResult{}, err
	}

	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if !dryRun {
		if err := os.MkdirAll(storageDir, 0o755); err != nil {
			return ProjectSyncResult{}, fmt.Errorf("create project storage: %w", err)
		}
		if err := ensureProjectMarker(storageDir, id); err != nil {
			return ProjectSyncResult{}, err
		}
	}

	effective, err := ps.effectivePatterns(root)
	if err != nil {
		return ProjectSyncResult{}, err
	}

	tracked, err := projectTrackedFiles(ctx, root)
	if err != nil {
		return ProjectSyncResult{}, err
	}

	result := ProjectSyncResult{ProjectID: id}

	stats, err := syncNewMatches(&fspkg.FileSystem{}, root, storageDir, effective, tracked, force, dryRun)
	if err != nil {
		return result, err
	}
	result.Synced = stats.synced
	result.SkippedTracked = stats.skippedTracked

	result.Released, result.BackedUp, err = ps.releaseUnmatched(ctx, root, effective, dryRun)
	if err != nil {
		return result, err
	}

	result.Deletions, result.Pruned, err = ps.liveDeletions(ctx, root, effective, dryRun, pruneDeletions)
	if err != nil {
		return result, err
	}

	if !dryRun {
		if err := ps.commitProjectChanges(ctx, "lnk: sync project "+id); err != nil {
			return result, err
		}
	}

	if len(stats.failed) > 0 {
		return result, fmt.Errorf("%w: %w", lnkerror.ErrSyncFailed, errors.Join(stats.failed...))
	}

	if err := ps.recordProjectCache(ctx, root, id); err != nil {
		return result, err
	}

	return result, nil
}

// ProjectSyncAllResult reports the outcome of syncing every stored project.
type ProjectSyncAllResult struct {
	// Results contains the per-project reconciliation outcomes for projects
	// listed as available in the local cache.
	Results []ProjectSyncResult
	// Unavailable lists stored project IDs whose cache entry is missing or not
	// available on this machine.
	Unavailable []string
}

// ProjectSyncAll reconciles every stored project that is marked available in
// the machine-local project registry. Projects marked not-downloaded or
// missing, or projects with no registry entry, are reported in Unavailable.
// Errors from individual projects are joined and returned alongside the
// partial result.
func (ps *ProjectService) ProjectSyncAll(ctx context.Context, dryRun, pruneDeletions, force bool) (ProjectSyncAllResult, error) {
	stored, err := ps.ProjectListProjects()
	if err != nil {
		return ProjectSyncAllResult{}, err
	}
	if len(stored) == 0 {
		return ProjectSyncAllResult{}, nil
	}

	cache, err := ps.LoadProjectCache()
	if err != nil {
		return ProjectSyncAllResult{}, err
	}

	result := ProjectSyncAllResult{}
	var errs []error
	for _, p := range stored {
		entry, ok := cache.Get(p.ID)
		if !ok || entry.State != CacheStateAvailable {
			result.Unavailable = append(result.Unavailable, p.ID)
			continue
		}

		res, err := ps.ProjectSync(ctx, entry.Path, dryRun, pruneDeletions, force)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.ID, err))
		}
		result.Results = append(result.Results, res)
	}

	return result, errors.Join(errs...)
}

// discoverProjectRoots scans scanRoots for git working trees and returns a map
// from project ID to the discovered local checkout paths. Hidden directories
// (except .git itself) are skipped, nested git directories are skipped once a
// repo root is found, and the lnk repository itself is excluded.
func (ps *ProjectService) discoverProjectRoots(ctx context.Context, scanRoots []string) (map[string][]string, error) {
	result := make(map[string][]string)
	seen := make(map[string]struct{})

	for _, root := range scanRoots {
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("scan root %s: %w", root, err)
		}
		if !info.IsDir() {
			continue
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve scan root %s: %w", root, err)
		}

		if err := filepath.Walk(absRoot, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fi.IsDir() && path != absRoot {
				name := fi.Name()
				// Skip hidden directories except the .git directories we are
				// explicitly looking for.
				if strings.HasPrefix(name, ".") && name != ".git" {
					return filepath.SkipDir
				}
				// Avoid descending into the lnk repo or its storage.
				if ps.svc.isLnkRepoRoot(path) {
					return filepath.SkipDir
				}
				// Bound the scan depth to avoid walking huge trees.
				rel, err := filepath.Rel(absRoot, path)
				if err != nil {
					return err
				}
				if depth(rel) > 3 {
					return filepath.SkipDir
				}
			}

			if !fi.IsDir() || fi.Name() != ".git" {
				return nil
			}

			repoRoot := filepath.Dir(path)
			if _, ok := seen[repoRoot]; ok {
				return filepath.SkipDir
			}
			seen[repoRoot] = struct{}{}

			if ps.svc.isLnkRepoRoot(repoRoot) {
				return filepath.SkipDir
			}

			id, err := ps.projectID(ctx, repoRoot)
			if err != nil {
				// Repos without a resolvable ID are ignored.
				if errors.Is(err, resolver.ErrNoOrigin) {
					return filepath.SkipDir
				}
				return err
			}
			result[id] = append(result[id], repoRoot)
			return filepath.SkipDir
		}); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// depth counts the number of path components in rel, which is assumed to use
// the local separator.
func depth(rel string) int {
	if rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// ProjectRemoveResult reports the outcome of ProjectRemove.
type ProjectRemoveResult struct {
	ProjectID string
	// Restored lists files moved back from storage to their live paths.
	Restored []string
	// BackedUp lists live files renamed to .lnk-backup during the move-back.
	BackedUp []string
}

// ProjectRemove stops managing the whole project: every stored file is moved
// back to its live path (existing live files are backed up first), the
// project's storage directory is deleted, and the removal is committed in
// the lnk repo. The project's .lnkinclude is left in place so the project
// can be re-adopted later with 'lnk project push'.
func (ps *ProjectService) ProjectRemove(ctx context.Context, projectRoot string) (ProjectRemoveResult, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return ProjectRemoveResult{}, err
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		return ProjectRemoveResult{}, err
	}

	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if _, err := os.Stat(storageDir); err != nil {
		return ProjectRemoveResult{}, lnkerror.WithPathAndSuggestion(lnkerror.ErrNotManaged, root, "no stored files for this project")
	}

	result := ProjectRemoveResult{ProjectID: id}
	fs := &fspkg.FileSystem{}
	var failed []error

	err = filepath.Walk(storageDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(storageDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == projectMarkerFile {
			return nil
		}

		backed, err := releaseFile(fs, path, filepath.Join(root, filepath.FromSlash(rel)), storageDir)
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", rel, err))
			return nil
		}
		result.Restored = append(result.Restored, rel)
		if backed {
			result.BackedUp = append(result.BackedUp, rel)
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	// Keep the storage directory (and skip the commit) when some files could
	// not be moved back, so nothing is lost.
	if len(failed) > 0 {
		return result, fmt.Errorf("%w: %w", lnkerror.ErrSyncFailed, errors.Join(failed...))
	}

	if err := os.RemoveAll(storageDir); err != nil {
		return result, fmt.Errorf("remove project storage: %w", err)
	}

	if err := ps.commitProjectChanges(ctx, "lnk: removed project "+id); err != nil {
		return result, err
	}

	return result, nil
}

// ProjectForgetResult reports the outcome of ProjectForget.
type ProjectForgetResult struct {
	ProjectID string
	// Unlinked lists live symlinks pointing into project storage that were
	// removed.
	Unlinked []string
}

// ProjectForget stops managing the whole project but keeps its stored files:
// live symlinks pointing into the project's storage are removed while the
// storage copy (and .lnkinclude) stay in place, so 'lnk project restore' can
// bring the files back later. Live real files are never touched.
func (ps *ProjectService) ProjectForget(ctx context.Context, projectRoot string) (ProjectForgetResult, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return ProjectForgetResult{}, err
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		return ProjectForgetResult{}, err
	}

	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if _, err := os.Stat(storageDir); err != nil {
		return ProjectForgetResult{}, lnkerror.WithPathAndSuggestion(lnkerror.ErrNotManaged, root, "no stored files for this project")
	}

	result := ProjectForgetResult{ProjectID: id}

	err = filepath.Walk(storageDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(storageDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == projectMarkerFile {
			return nil
		}

		livePath := filepath.Join(root, filepath.FromSlash(rel))
		liveInfo, statErr := os.Lstat(livePath)
		if statErr != nil || liveInfo.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		if !isStorageSymlink(livePath, storageDir) {
			return nil
		}
		if err := os.Remove(livePath); err != nil {
			return fmt.Errorf("remove managed symlink %s: %w", livePath, err)
		}
		result.Unlinked = append(result.Unlinked, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return result, err
	}

	return result, nil
}

// ensureProjectMarker writes the .lnkproject marker into a project's storage
// root, recording its ID so stored projects can be enumerated even though
// IDs contain slashes.
func ensureProjectMarker(storageDir, id string) error {
	marker := filepath.Join(storageDir, projectMarkerFile)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	if err := os.WriteFile(marker, []byte(id+"\n"), 0o644); err != nil {
		return fmt.Errorf("write project marker: %w", err)
	}
	return nil
}

// ProjectAddGlobalPattern appends a pattern to the lnk repo's global
// .lnkinclude. Global patterns are stored verbatim (they apply to every
// project, so they cannot be relativized against one root) and are not
// match-checked.
func (ps *ProjectService) ProjectAddGlobalPattern(rawPattern string) (string, error) {
	pattern := strings.TrimSpace(rawPattern)
	if pattern == "" || strings.TrimPrefix(pattern, "!") == "" {
		return "", lnkerror.Wrap(lnkerror.ErrEmptyPattern)
	}

	manifest := filepath.Join(ps.svc.RepoPath(), ".lnkinclude")
	existing, err := patterns.Load(manifest)
	if err != nil {
		return "", fmt.Errorf("load global .lnkinclude: %w", err)
	}
	if slices.Contains(existing, pattern) {
		return "", lnkerror.WithPath(lnkerror.ErrAlreadyManaged, pattern)
	}

	if err := appendPattern(manifest, pattern); err != nil {
		return "", err
	}
	return pattern, nil
}

// ProjectUntrackGlobalPattern removes a pattern from the lnk repo's global
// .lnkinclude. It returns removed=true when the pattern was present.
func (ps *ProjectService) ProjectUntrackGlobalPattern(pattern string) (bool, error) {
	manifest := filepath.Join(ps.svc.RepoPath(), ".lnkinclude")
	lines, err := patterns.Load(manifest)
	if err != nil {
		return false, fmt.Errorf("load global .lnkinclude: %w", err)
	}
	if !slices.Contains(lines, pattern) {
		return false, lnkerror.WithPath(lnkerror.ErrNotManaged, pattern)
	}
	if err := rewritePatterns(manifest, lines, pattern); err != nil {
		return false, err
	}
	return true, nil
}

// StoredProject describes one project discovered in lnk storage.
type StoredProject struct {
	ID    string
	Files int
}

// ProjectListProjects returns the projects discovered in lnk storage via
// their .lnkproject markers, sorted by ID.
func (ps *ProjectService) ProjectListProjects() ([]StoredProject, error) {
	root := filepath.Join(ps.svc.RepoPath(), "projects")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan projects: %w", err)
	}

	var result []StoredProject
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || info.Name() != projectMarkerFile {
			return nil
		}
		id, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		id = filepath.ToSlash(id)
		files, err := countProjectFiles(filepath.Dir(path))
		if err != nil {
			return err
		}
		result = append(result, StoredProject{ID: id, Files: files})
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(result, func(a, b StoredProject) int {
		return strings.Compare(a.ID, b.ID)
	})
	return result, nil
}

// ProjectHealth captures the storage-side health of one project for doctor.
type ProjectHealth struct {
	ID    string
	Files int
}

// scanProjects returns the stored projects, top-level storage directories
// without a marker, and marked projects with no files. Plain files at the
// top of projects/ (e.g. a stray .DS_Store) are ignored, not reported.
func (s *Service) scanProjects() (projects []ProjectHealth, unmarked, empty []string, err error) {
	root := filepath.Join(s.repoPath, "projects")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("scan projects: %w", err)
	}

	markerDirs := make(map[string]struct{})
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || info.Name() != projectMarkerFile {
			return nil
		}
		markerDirs[filepath.Dir(path)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}

	for dir := range markerDirs {
		id, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, nil, nil, err
		}
		id = filepath.ToSlash(id)
		files, err := countProjectFiles(dir)
		if err != nil {
			return nil, nil, nil, err
		}
		if files == 0 {
			empty = append(empty, id)
		} else {
			projects = append(projects, ProjectHealth{ID: id, Files: files})
		}
	}
	slices.SortFunc(projects, func(a, b ProjectHealth) int {
		return strings.Compare(a.ID, b.ID)
	})
	slices.Sort(empty)

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, e := range entries {
		// Non-directory entries (e.g. a stray .DS_Store) are not projects.
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		hasMarker, err := dirContainsMarker(filepath.Join(root, name))
		if err != nil {
			return nil, nil, nil, err
		}
		if hasMarker {
			continue
		}
		hasFiles, err := dirContainsFiles(filepath.Join(root, name))
		if err != nil {
			return nil, nil, nil, err
		}
		if hasFiles {
			unmarked = append(unmarked, name)
		}
	}
	slices.Sort(unmarked)

	return projects, unmarked, empty, nil
}

// countProjectFiles counts regular files under dir, excluding the project
// marker itself.
func countProjectFiles(dir string) (int, error) {
	n := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || info.Name() == projectMarkerFile {
			return nil
		}
		n++
		return nil
	})
	return n, err
}

func dirContainsMarker(dir string) (bool, error) {
	found := false
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && info.Name() == projectMarkerFile {
			found = true
		}
		return nil
	})
	return found, err
}

func dirContainsFiles(dir string) (bool, error) {
	found := false
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			found = true
		}
		return nil
	})
	return found, err
}

// pruneEmptyProjects removes the storage directory of each empty project and
// returns the pruned IDs and the marker paths to stage for removal.
func (s *Service) pruneEmptyProjects(ids []string) ([]string, []string, error) {
	var pruned []string
	var stagePaths []string
	for _, id := range ids {
		dir := filepath.Join(s.repoPath, "projects", filepath.FromSlash(id))
		if err := os.RemoveAll(dir); err != nil {
			return nil, nil, fmt.Errorf("remove empty project storage %s: %w", id, err)
		}
		stagePaths = append(stagePaths, filepath.ToSlash(filepath.Join("projects", id, projectMarkerFile)))
		pruned = append(pruned, id)
	}
	return pruned, stagePaths, nil
}

// ProjectRestore recreates symlinks for all project-scoped files from
// storage. Live files tracked by the project's own git index are left
// untouched (and reported in RestoreInfo.SkippedTracked) unless force is
// set, since replacing them with symlinks would dirty the project's working
// tree with a typechange.
func (ps *ProjectService) ProjectRestore(ctx context.Context, projectRoot string, dryRun, force bool) (RestoreInfo, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return RestoreInfo{}, err
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		return RestoreInfo{}, err
	}

	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		return RestoreInfo{}, lnkerror.WithPathAndSuggestion(lnkerror.ErrNotManaged, root, "run 'lnk project push' first")
	}

	tracked, err := projectTrackedFiles(ctx, root)
	if err != nil {
		return RestoreInfo{}, err
	}

	effective, err := ps.effectivePatterns(root)
	if err != nil {
		return RestoreInfo{}, err
	}

	r := &scope.ProjectRootResolver{
		GitRoot:    root,
		StorageDir: storageDir,
	}

	info := RestoreInfo{}
	fs := &fspkg.FileSystem{}

	err = filepath.Walk(storageDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(storageDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == projectMarkerFile {
			return nil
		}

		// Stored files whose patterns no longer match are drift, not state to
		// recreate; 'lnk project sync' moves them back.
		match, err := patterns.Match(effective, rel)
		if err != nil {
			return err
		}
		if !match {
			info.SkippedUnmatched = append(info.SkippedUnmatched, rel)
			return nil
		}

		livePath, err := r.ToLive(rel)
		if err != nil {
			return err
		}

		liveInfo, statErr := os.Lstat(livePath)
		liveExists := statErr == nil
		liveIsSymlink := liveExists && liveInfo.Mode()&os.ModeSymlink != 0

		if !force && !liveIsSymlink {
			if _, ok := tracked[rel]; ok {
				info.SkippedTracked = append(info.SkippedTracked, rel)
				return nil
			}
		}

		if liveExists {
			if liveIsSymlink {
				if isManagedSymlink(livePath, path) {
					return nil
				}
				if !dryRun {
					if err := os.Remove(livePath); err != nil {
						return fmt.Errorf("replace stale symlink %s: %w", livePath, err)
					}
				}
			} else {
				info.BackedUp = append(info.BackedUp, rel)
				if !dryRun {
					backupPath := livePath + ".lnk-backup"
					if _, err := os.Lstat(backupPath); err == nil {
						return lnkerror.WithPath(lnkerror.ErrBackupExists, backupPath)
					}
					if err := os.Rename(livePath, backupPath); err != nil {
						return fmt.Errorf("backup existing file %s: %w", livePath, err)
					}
				}
			}
		}

		info.Restored = append(info.Restored, rel)
		if dryRun {
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
			return fmt.Errorf("create live parent directory: %w", err)
		}
		if err := fs.CreateSymlink(path, livePath); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return info, err
	}

	return info, nil
}

// ProjectRestoreHook is the collision-safe variant of ProjectRestore used by
// the post-checkout git hook. It creates missing symlinks, skips symlinks
// that already point to the correct storage target, replaces symlinks that
// point elsewhere, and reports real-file collisions without backing them up.
// It never returns an error for a per-file collision; collisions are collected
// in RestoreInfo.Collisions and reported by the caller.
func (ps *ProjectService) ProjectRestoreHook(ctx context.Context, projectRoot string) (RestoreInfo, error) {
	root, err := ps.resolveProjectRoot(ctx, projectRoot)
	if err != nil {
		return RestoreInfo{}, err
	}

	id, err := ps.projectID(ctx, root)
	if err != nil {
		return RestoreInfo{}, err
	}

	storageDir := filepath.Join(ps.svc.RepoPath(), "projects", id)
	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		return RestoreInfo{}, nil
	}

	tracked, err := projectTrackedFiles(ctx, root)
	if err != nil {
		return RestoreInfo{}, err
	}

	effective, err := ps.effectivePatterns(root)
	if err != nil {
		return RestoreInfo{}, err
	}

	r := &scope.ProjectRootResolver{
		GitRoot:    root,
		StorageDir: storageDir,
	}

	info := RestoreInfo{}
	fs := &fspkg.FileSystem{}

	err = filepath.Walk(storageDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(storageDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == projectMarkerFile {
			return nil
		}

		match, err := patterns.Match(effective, rel)
		if err != nil {
			return err
		}
		if !match {
			return nil
		}

		livePath, err := r.ToLive(rel)
		if err != nil {
			return err
		}

		liveInfo, statErr := os.Lstat(livePath)
		liveExists := statErr == nil
		liveIsSymlink := liveExists && liveInfo.Mode()&os.ModeSymlink != 0

		if _, ok := tracked[rel]; ok && !liveIsSymlink {
			info.Collisions = append(info.Collisions, rel)
			return nil
		}

		if liveExists {
			if liveIsSymlink {
				if isManagedSymlink(livePath, path) {
					return nil
				}
				if err := os.Remove(livePath); err != nil {
					return fmt.Errorf("replace stale symlink %s: %w", livePath, err)
				}
			} else {
				info.Collisions = append(info.Collisions, rel)
				return nil
			}
		}

		info.Restored = append(info.Restored, rel)
		if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
			return fmt.Errorf("create live parent directory: %w", err)
		}
		if err := fs.CreateSymlink(path, livePath); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return info, err
	}

	return info, nil
}

// ProjectPull pulls the lnk repo and restores project symlinks.
func (ps *ProjectService) ProjectPull(ctx context.Context, projectRoot string, force bool) (RestoreInfo, error) {
	if err := ps.svc.git.Pull(ctx); err != nil {
		return RestoreInfo{}, err
	}
	return ps.ProjectRestore(ctx, projectRoot, false, force)
}
