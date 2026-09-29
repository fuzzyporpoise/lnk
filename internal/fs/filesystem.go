// Package fs provides file system operations for lnk.
package fs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

// Sentinel errors for file system operations.
var (
	ErrFileNotExists   = errors.New("file or directory not found")
	ErrFileCheck       = errors.New("unable to access file")
	ErrUnsupportedType = errors.New("cannot manage this type of file")
	ErrSymlinkRead     = errors.New("unable to read symlink")
	ErrDirCreate       = errors.New("failed to create directory")
	ErrRelativePath    = errors.New("unable to create symlink")
)

// FileSystem handles file system operations
type FileSystem struct{}

// ValidateFileInfoForAdd validates that a file or directory can be added to lnk.
func (fs *FileSystem) ValidateFileInfoForAdd(filePath string) (os.FileInfo, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, lnkerror.WithPath(ErrFileNotExists, filePath)
		}

		return nil, lnkerror.WithPathAndSuggestion(ErrFileCheck, filePath, "check file permissions and try again")
	}

	// Reject symlinks explicitly
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, lnkerror.WithPathAndSuggestion(ErrUnsupportedType, filePath, "lnk can only manage regular files and directories")
	}

	if !info.Mode().IsRegular() && !info.IsDir() {
		return nil, lnkerror.WithPathAndSuggestion(ErrUnsupportedType, filePath, "lnk can only manage regular files and directories")
	}

	return info, nil
}

// ValidateSymlinkForRemove validates that a symlink can be removed from lnk
func (fs *FileSystem) ValidateSymlinkForRemove(filePath, repoPath string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return lnkerror.WithPath(ErrFileNotExists, filePath)
		}

		return lnkerror.WithPathAndSuggestion(ErrFileCheck, filePath, "check file permissions and try again")
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return lnkerror.WithPathAndSuggestion(lnkerror.ErrNotManaged, filePath, "use 'lnk add' to manage this file first")
	}

	target, err := os.Readlink(filePath)
	if err != nil {
		return lnkerror.WithPathAndSuggestion(ErrSymlinkRead, filePath, "the file may be corrupted or have invalid permissions")
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(filePath), target)
	}

	target = filepath.Clean(target)
	repoPath = filepath.Clean(repoPath)

	if !strings.HasPrefix(target, repoPath+string(filepath.Separator)) && target != repoPath {
		return lnkerror.WithPathAndSuggestion(lnkerror.ErrNotManaged, filePath, "use 'lnk add' to manage this file first")
	}

	return nil
}

// Move moves a file or directory from source to destination based on the file info
func (fs *FileSystem) Move(src, dst string, info os.FileInfo) error {
	if info.IsDir() {
		return fs.MoveDirectory(src, dst)
	}
	return fs.MoveFile(src, dst)
}

// MoveFile moves a file from source to destination
func (fs *FileSystem) MoveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return lnkerror.WithPathAndSuggestion(ErrDirCreate, filepath.Dir(dst), "check permissions and available disk space")
	}

	return os.Rename(src, dst)
}

// CreateSymlink creates a relative symlink from target to linkPath
func (fs *FileSystem) CreateSymlink(target, linkPath string) error {
	relTarget, err := filepath.Rel(filepath.Dir(linkPath), target)
	if err != nil {
		return lnkerror.WithSuggestion(ErrRelativePath, "check file locations")
	}

	return os.Symlink(relTarget, linkPath)
}

// MoveDirectory moves a directory from source to destination recursively
func (fs *FileSystem) MoveDirectory(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return lnkerror.WithPathAndSuggestion(ErrDirCreate, filepath.Dir(dst), "check permissions and available disk space")
	}

	return os.Rename(src, dst)
}

// RemoveEmptyDirs removes all empty directories underneath root path
func RemoveEmptyDirs(rootPath string) error {
	var dirs []string

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != rootPath {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(dirs, func(a, b string) int {
		return strings.Count(b, string(os.PathSeparator)) - strings.Count(a, string(os.PathSeparator))
	})

	for _, dir := range dirs {
		isEmpty, err := isDirEmpty(dir)
		if err != nil {
			return err
		}
		if isEmpty {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}

	return nil
}

// isDirEmpty reports whether a directory contains any entries.
func isDirEmpty(name string) (bool, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
