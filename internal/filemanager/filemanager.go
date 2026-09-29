// Package filemanager handles adding and removing files from lnk management.
package filemanager

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

type fileSystem interface {
	ValidateFileInfoForAdd(path string) (os.FileInfo, error)
	Move(src, dst string, info os.FileInfo) error
	CreateSymlink(target, link string) error
	ValidateSymlinkForRemove(absPath, repoPath string) error
}

type tracker interface {
	LnkFileName() (string, error)
	HostStoragePath() (string, error)
	HostStorageRelPath() (string, error)
	AddManagedItem(path string) error
	RemoveManagedItem(path string) error
	GetManagedItems() ([]string, error)
}

// Manager handles adding and removing files from lnk management.
type Manager struct {
	repoPath string
	host     string
	fs       fileSystem
	tracker  tracker
}

// New creates a new file Manager.
func New(repoPath, host string, f fileSystem, t tracker) *Manager {
	return &Manager{
		repoPath: repoPath,
		host:     host,
		fs:       f,
		tracker:  t,
	}
}

// validatedFile holds pre-validated file information for batch operations.
type validatedFile struct {
	absPath      string
	relativePath string
	info         os.FileInfo
}

type AddResult struct {
	StagePaths []string       // tracker file + repo storage paths
	Rollback   []func() error // rollback actions in case commit fails
}

type RemoveResult struct {
	StagePaths  []string     // tracker file
	RemovePaths []string     // paths to `git rm --cached`
	IsDir       bool         // used to remove directory
	RestoreFn   func() error // moves file back after git work is done
}

type FileToTrack struct {
	AbsPath      string
	RelativePath string
}

// AddMultiple adds multiple files in a single transaction with optional progress reporting.
func (fm *Manager) AddMultiple(paths []FileToTrack) (AddResult, error) {
	if len(paths) == 0 {
		return AddResult{}, nil
	}

	files, err := fm.validatePaths(paths)
	if err != nil {
		return AddResult{}, err
	}

	trackerFile, err := fm.tracker.LnkFileName()
	if err != nil {
		return AddResult{}, err
	}

	stagePaths, rollbackActions, err := fm.processFiles(files)
	if err != nil {
		return AddResult{}, err
	}

	stageFiles := append(stagePaths, trackerFile)

	return AddResult{
		StagePaths: stageFiles,
		Rollback:   rollbackActions,
	}, nil
}

// validatePaths validates all paths and returns validated file info.
func (fm *Manager) validatePaths(paths []FileToTrack) ([]validatedFile, error) {
	var files []validatedFile

	for _, path := range paths {
		info, err := fm.fs.ValidateFileInfoForAdd(path.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("validation failed for %s: %w", path.AbsPath, err)
		}

		files = append(files, validatedFile{
			absPath:      path.AbsPath,
			relativePath: path.RelativePath,
			info:         info,
		})
	}

	return files, nil
}

// processFiles moves files to the repo, creates symlinks, and updates tracking.
func (fm *Manager) processFiles(files []validatedFile) ([]string, []func() error, error) {
	var rollbackActions []func() error
	var paths []string

	for _, f := range files {
		storagePath, err := fm.tracker.HostStoragePath()
		if err != nil {
			return nil, nil, err
		}
		destPath := filepath.Join(storagePath, f.relativePath)

		storageRelPath, err := fm.tracker.HostStorageRelPath()
		if err != nil {
			return nil, nil, err
		}
		relPath := filepath.Join(storageRelPath, f.relativePath)

		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			fm.RollbackAll(rollbackActions)
			return nil, nil, fmt.Errorf("failed to create destination directory: %w", err)
		}

		if err := fm.fs.Move(f.absPath, destPath, f.info); err != nil {
			fm.RollbackAll(rollbackActions)
			return nil, nil, fmt.Errorf("failed to move %s: %w", f.absPath, err)
		}

		if err := fm.fs.CreateSymlink(destPath, f.absPath); err != nil {
			_ = fm.fs.Move(destPath, f.absPath, f.info)
			fm.RollbackAll(rollbackActions)
			return nil, nil, fmt.Errorf("failed to create symlink for %s: %w", f.absPath, err)
		}

		if err := fm.tracker.AddManagedItem(f.relativePath); err != nil {
			_ = os.Remove(f.absPath)
			_ = fm.fs.Move(destPath, f.absPath, f.info)
			fm.RollbackAll(rollbackActions)
			return nil, nil, fmt.Errorf("failed to update tracking file for %s: %w", f.absPath, err)
		}

		rollbackActions = append(rollbackActions, fm.createRollbackAction(f.absPath, destPath, f.relativePath, f.info))
		paths = append(paths, relPath)
	}

	return paths, rollbackActions, nil
}

// createRollbackAction creates a rollback function for a single file operation.
func (fm *Manager) createRollbackAction(absPath, destPath, relativePath string, info os.FileInfo) func() error {
	return func() error {
		_ = os.Remove(absPath)
		_ = fm.tracker.RemoveManagedItem(relativePath)
		return fm.fs.Move(destPath, absPath, info)
	}
}

// RollbackAll executes rollback actions in reverse order.
func (fm *Manager) RollbackAll(actions []func() error) {
	for i := len(actions) - 1; i >= 0; i-- {
		_ = actions[i]()
	}
}

// Remove removes a symlink and restores the original file or directory.
func (fm *Manager) Remove(file FileToTrack) (RemoveResult, error) {
	if err := fm.fs.ValidateSymlinkForRemove(file.AbsPath, fm.repoPath); err != nil {
		return RemoveResult{}, err
	}

	managedItems, err := fm.tracker.GetManagedItems()
	if err != nil {
		return RemoveResult{}, fmt.Errorf("failed to get managed items: %w", err)
	}

	trackerFile, err := fm.tracker.LnkFileName()
	if err != nil {
		return RemoveResult{}, err
	}

	if !slices.Contains(managedItems, file.RelativePath) {
		return RemoveResult{}, lnkerror.WithPath(lnkerror.ErrNotManaged, file.RelativePath)
	}

	target, err := os.Readlink(file.AbsPath)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("failed to read symlink: %w", err)
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(file.AbsPath), target)
	}

	info, err := os.Stat(target)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("failed to stat target: %w", err)
	}

	if err := os.Remove(file.AbsPath); err != nil {
		return RemoveResult{}, fmt.Errorf("failed to remove symlink: %w", err)
	}

	if err := fm.tracker.RemoveManagedItem(file.RelativePath); err != nil {
		return RemoveResult{}, fmt.Errorf("failed to update tracking file: %w", err)
	}

	return RemoveResult{
		StagePaths:  []string{trackerFile},
		RemovePaths: []string{target},
		RestoreFn:   func() error { return fm.fs.Move(target, file.AbsPath, info) },
		IsDir:       info.IsDir(),
	}, nil
}
