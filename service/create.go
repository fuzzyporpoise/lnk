package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.fuzzyporpoise.dev/lnk/v2/internal/filemanager"
	"go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

const lnkKeepFileName = ".lnkkeep"

// CreateOptions configures the Create command.
type CreateOptions struct {
	// AsDir forces every path to be treated as a directory.
	AsDir bool
}

// Create creates empty files or directories and tracks them in common or one
// host scope. All paths must be of the same kind within a single invocation.
func (s *Service) Create(ctx context.Context, host string, paths []string, opts CreateOptions) error {
	if err := s.requireGitRepo(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return lnkerror.Wrap(lnkerror.ErrNoPaths)
	}

	host = NormalizeHost(host)
	seen := make(map[string]struct{}, len(paths))
	var files []filemanager.FileToTrack

	for _, input := range paths {
		file, err := s.homeRelativePath(input)
		if err != nil {
			return err
		}
		if _, ok := seen[file.RelativePath]; ok {
			return lnkerror.WithPath(lnkerror.ErrDuplicatePath, file.RelativePath)
		}
		seen[file.RelativePath] = struct{}{}

		checkPath, err := existingAncestor(file.AbsPath)
		if err != nil {
			return lnkerror.WithPathAndSuggestion(fs.ErrFileCheck, file.AbsPath, "check file permissions and try again")
		}
		inside, gitRoot, err := gitboundary.IsInsideGitRepo(ctx, checkPath)
		if err != nil {
			return err
		}
		if inside {
			return lnkerror.WithPathAndSuggestion(lnkerror.ErrInsideGitRepo, file.RelativePath, fmt.Sprintf("inside git repo %s; use 'lnk project add' from within the project", gitRoot))
		}

		owner, err := s.findOwner(file.RelativePath)
		if err != nil {
			return err
		}
		if owner != nil {
			return lnkerror.WithPathAndSuggestion(lnkerror.ErrAlreadyManaged, file.RelativePath, fmt.Sprintf("already managed in scope %s", owner.Host))
		}

		ancestor, err := s.findAncestorOwner(file.RelativePath)
		if err != nil {
			return err
		}
		if ancestor != nil {
			return lnkerror.WithPathAndSuggestion(lnkerror.ErrAlreadyManaged, file.RelativePath, fmt.Sprintf("inside a directory already managed in scope %s", ancestor.Host))
		}

		if err := createPath(file.AbsPath, opts.AsDir); err != nil {
			return err
		}

		files = append(files, filemanager.FileToTrack{
			AbsPath:      file.AbsPath,
			RelativePath: file.RelativePath,
		})
	}

	fm, err := s.fileManager(host)
	if err != nil {
		return err
	}

	addResult, err := fm.AddMultiple(files)
	if err != nil {
		return err
	}

	if err := s.stagePaths(ctx, addResult.StagePaths...); err != nil {
		return err
	}

	pathCommit := strings.Join(addResult.StagePaths, "\n")
	if err := s.commit(ctx, fmt.Sprintf("lnk: created and added to %s\n%s", host, pathCommit)); err != nil {
		fm.RollbackAll(addResult.Rollback)
		return err
	}

	return nil
}

// createPath creates an empty file or directory at absPath. For directories it
// also writes a .lnkkeep placeholder so git tracks the empty directory.
func createPath(absPath string, asDir bool) error {
	info, err := os.Lstat(absPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return lnkerror.WithPathAndSuggestion(fs.ErrUnsupportedType, absPath, "lnk cannot create over a symlink")
		}
		if asDir {
			if !info.IsDir() {
				return lnkerror.WithPathAndSuggestion(fs.ErrUnsupportedType, absPath, "path exists and is not a directory")
			}
			empty, err := isDirEmptyOrKeepOnly(absPath)
			if err != nil {
				return lnkerror.WithPathAndSuggestion(fs.ErrFileCheck, absPath, "check file permissions and try again")
			}
			if !empty {
				return lnkerror.WithPathAndSuggestion(lnkerror.ErrPathExists, absPath, "directory is not empty; use 'lnk add' to manage existing directories")
			}
		} else {
			return lnkerror.WithPathAndSuggestion(lnkerror.ErrPathExists, absPath, "use 'lnk add' to manage existing files")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return lnkerror.WithPathAndSuggestion(fs.ErrFileCheck, absPath, "check file permissions and try again")
	}

	if asDir {
		if err := os.MkdirAll(absPath, 0o755); err != nil {
			return lnkerror.WithPathAndSuggestion(fs.ErrDirCreate, absPath, "check permissions and available disk space")
		}
		keepPath := filepath.Join(absPath, lnkKeepFileName)
		if _, err := os.Lstat(keepPath); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(keepPath, []byte{}, 0o644); err != nil {
				return lnkerror.WithPathAndSuggestion(fs.ErrFileCheck, keepPath, "check file permissions and available disk space")
			}
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return lnkerror.WithPathAndSuggestion(fs.ErrDirCreate, filepath.Dir(absPath), "check permissions and available disk space")
	}

	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return lnkerror.WithPathAndSuggestion(lnkerror.ErrPathExists, absPath, "use 'lnk add' to manage existing files")
		}
		return lnkerror.WithPathAndSuggestion(fs.ErrFileCheck, absPath, "check file permissions and available disk space")
	}
	_ = f.Close()
	return nil
}

// existingAncestor walks up from absPath until it finds an existing file or
// directory and returns the path to check for git-boundary membership. For a
// file it returns the file itself; for a missing path it returns the deepest
// existing parent directory.
func existingAncestor(absPath string) (string, error) {
	for {
		info, err := os.Lstat(absPath)
		if err == nil {
			if info.IsDir() {
				return absPath, nil
			}
			return filepath.Dir(absPath), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(absPath)
		if parent == absPath {
			return "", err
		}
		absPath = parent
	}
}

// isDirEmptyOrKeepOnly reports whether dir contains no entries, or only a
// .lnkkeep placeholder.
func isDirEmptyOrKeepOnly(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() != lnkKeepFileName {
			return false, nil
		}
	}
	return true, nil
}

// findAncestorOwner returns the first scope that manages an ancestor of the
// given relative path, or nil if none.
func (s *Service) findAncestorOwner(relativePath string) (*owner, error) {
	for {
		dir := filepath.Dir(relativePath)
		if dir == relativePath || dir == "." {
			break
		}
		relativePath = dir
		owner, err := s.findOwner(relativePath)
		if err != nil {
			return nil, err
		}
		if owner != nil {
			return owner, nil
		}
	}
	return nil, nil
}
