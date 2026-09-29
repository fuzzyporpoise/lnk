// internal/filemanager/filemanager_test.go
package filemanager_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.fuzzyporpoise.dev/lnk/v2/internal/filemanager"
)

// ---------- fakes ----------

type fakeFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	dir     bool
}

func (f *fakeFileInfo) Name() string       { return f.name }
func (f *fakeFileInfo) Size() int64        { return f.size }
func (f *fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f *fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f *fakeFileInfo) IsDir() bool        { return f.dir }
func (f *fakeFileInfo) Sys() any           { return nil }

type moveCall struct{ src, dst string }

type fakeFileSystem struct {
	validateFileInfoFunc func(path string) (os.FileInfo, error)
	moveFunc             func(src, dst string, info os.FileInfo) error
	moveCalls            []moveCall
	createSymlinkFunc    func(target, link string) error
	validateSymlinkFunc  func(absPath, repoPath string) error
}

func (f *fakeFileSystem) ValidateFileInfoForAdd(path string) (os.FileInfo, error) {
	if f.validateFileInfoFunc != nil {
		return f.validateFileInfoFunc(path)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeFileSystem) Move(src, dst string, info os.FileInfo) error {
	f.moveCalls = append(f.moveCalls, moveCall{src, dst})
	if f.moveFunc != nil {
		return f.moveFunc(src, dst, info)
	}
	return nil
}

func (f *fakeFileSystem) CreateSymlink(target, link string) error {
	if f.createSymlinkFunc != nil {
		return f.createSymlinkFunc(target, link)
	}
	return nil
}

func (f *fakeFileSystem) ValidateSymlinkForRemove(absPath, repoPath string) error {
	if f.validateSymlinkFunc != nil {
		return f.validateSymlinkFunc(absPath, repoPath)
	}
	return nil
}

type fakeTracker struct {
	lnkFileNameFunc        func() (string, error)
	hostStoragePathFunc    func() (string, error)
	hostStorageRelPathFunc func() (string, error)
	addManagedItemFunc     func(path string) error
	removeManagedItemFunc  func(path string) error
	getManagedItemsFunc    func() ([]string, error)
}

func (f *fakeTracker) LnkFileName() (string, error) {
	if f.lnkFileNameFunc != nil {
		return f.lnkFileNameFunc()
	}
	return "", errors.New("not implemented")
}

func (f *fakeTracker) HostStoragePath() (string, error) {
	if f.hostStoragePathFunc != nil {
		return f.hostStoragePathFunc()
	}
	return "", errors.New("not implemented")
}

func (f *fakeTracker) HostStorageRelPath() (string, error) {
	if f.hostStorageRelPathFunc != nil {
		return f.hostStorageRelPathFunc()
	}
	return "", errors.New("not implemented")
}

func (f *fakeTracker) AddManagedItem(path string) error {
	if f.addManagedItemFunc != nil {
		return f.addManagedItemFunc(path)
	}
	return nil
}

func (f *fakeTracker) RemoveManagedItem(path string) error {
	if f.removeManagedItemFunc != nil {
		return f.removeManagedItemFunc(path)
	}
	return nil
}

func (f *fakeTracker) GetManagedItems() ([]string, error) {
	if f.getManagedItemsFunc != nil {
		return f.getManagedItemsFunc()
	}
	return nil, errors.New("not implemented")
}

// ---------- tests ----------

func TestManager_AddMultiple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		paths      []filemanager.FileToTrack
		fsSetup    func(t *testing.T, storage string) *fakeFileSystem
		trkSetup   func(t *testing.T, storage string) *fakeTracker
		wantErr    bool
		errMsg     string
		wantStages int
		wantRollbk int
		wantMoves  int
	}{
		{
			name:       "empty_paths_returns_empty_result",
			paths:      nil,
			fsSetup:    func(t *testing.T, s string) *fakeFileSystem { return &fakeFileSystem{} },
			trkSetup:   func(t *testing.T, s string) *fakeTracker { return &fakeTracker{} },
			wantStages: 0,
			wantRollbk: 0,
		},
		{
			name: "validation_failure_returns_error",
			paths: []filemanager.FileToTrack{
				{AbsPath: "/foo", RelativePath: "foo"},
			},
			fsSetup: func(t *testing.T, s string) *fakeFileSystem {
				return &fakeFileSystem{
					validateFileInfoFunc: func(path string) (os.FileInfo, error) {
						return nil, errors.New("not a regular file")
					},
				}
			},
			trkSetup: func(t *testing.T, s string) *fakeTracker { return &fakeTracker{} },
			wantErr:  true,
			errMsg:   "validation failed",
		},
		{
			name: "success_returns_stage_paths_and_rollback",
			paths: []filemanager.FileToTrack{
				{AbsPath: "/foo", RelativePath: "foo"},
			},
			fsSetup: func(t *testing.T, s string) *fakeFileSystem {
				return &fakeFileSystem{
					validateFileInfoFunc: func(path string) (os.FileInfo, error) {
						return &fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
					},
					moveFunc:          func(src, dst string, info os.FileInfo) error { return nil },
					createSymlinkFunc: func(target, link string) error { return nil },
				}
			},
			trkSetup: func(t *testing.T, s string) *fakeTracker {
				return &fakeTracker{
					lnkFileNameFunc:        func() (string, error) { return filepath.Join("repo", ".lnk"), nil },
					hostStoragePathFunc:    func() (string, error) { return s, nil },
					hostStorageRelPathFunc: func() (string, error) { return "storage", nil },
					addManagedItemFunc:     func(path string) error { return nil },
				}
			},
			wantStages: 2,
			wantRollbk: 1,
		},
		{
			name: "rollback_on_move_failure",
			paths: []filemanager.FileToTrack{
				{AbsPath: "/file1", RelativePath: "file1"},
				{AbsPath: "/file2", RelativePath: "file2"},
			},
			fsSetup: func(t *testing.T, s string) *fakeFileSystem {
				return &fakeFileSystem{
					validateFileInfoFunc: func(path string) (os.FileInfo, error) {
						return &fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
					},
					moveFunc: func(src, dst string, info os.FileInfo) error {
						if strings.Contains(dst, "file2") {
							return errors.New("disk full")
						}
						return nil
					},
					createSymlinkFunc: func(target, link string) error { return nil },
				}
			},
			trkSetup: func(t *testing.T, s string) *fakeTracker {
				return &fakeTracker{
					lnkFileNameFunc:        func() (string, error) { return ".lnk", nil },
					hostStoragePathFunc:    func() (string, error) { return s, nil },
					hostStorageRelPathFunc: func() (string, error) { return "storage", nil },
					addManagedItemFunc:     func(path string) error { return nil },
				}
			},
			wantErr:   true,
			wantMoves: 3, // 2 forward + 1 rollback
		},
		{
			name: "rollback_on_symlink_failure",
			paths: []filemanager.FileToTrack{
				{AbsPath: "/foo", RelativePath: "foo"},
			},
			fsSetup: func(t *testing.T, s string) *fakeFileSystem {
				return &fakeFileSystem{
					validateFileInfoFunc: func(path string) (os.FileInfo, error) {
						return &fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
					},
					moveFunc:          func(src, dst string, info os.FileInfo) error { return nil },
					createSymlinkFunc: func(target, link string) error { return errors.New("permission denied") },
				}
			},
			trkSetup: func(t *testing.T, s string) *fakeTracker {
				return &fakeTracker{
					lnkFileNameFunc:        func() (string, error) { return ".lnk", nil },
					hostStoragePathFunc:    func() (string, error) { return s, nil },
					hostStorageRelPathFunc: func() (string, error) { return "storage", nil },
					addManagedItemFunc:     func(path string) error { return nil },
				}
			},
			wantErr:   true,
			errMsg:    "symlink",
			wantMoves: 2, // forward + rollback
		},
		{
			name: "rollback_on_tracker_failure",
			paths: []filemanager.FileToTrack{
				{AbsPath: "/foo", RelativePath: "foo"},
			},
			fsSetup: func(t *testing.T, s string) *fakeFileSystem {
				return &fakeFileSystem{
					validateFileInfoFunc: func(path string) (os.FileInfo, error) {
						return &fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
					},
					moveFunc:          func(src, dst string, info os.FileInfo) error { return nil },
					createSymlinkFunc: func(target, link string) error { return nil },
				}
			},
			trkSetup: func(t *testing.T, s string) *fakeTracker {
				return &fakeTracker{
					lnkFileNameFunc:        func() (string, error) { return ".lnk", nil },
					hostStoragePathFunc:    func() (string, error) { return s, nil },
					hostStorageRelPathFunc: func() (string, error) { return "storage", nil },
					addManagedItemFunc: func(path string) error {
						return errors.New("tracker locked")
					},
				}
			},
			wantErr:   true,
			errMsg:    "tracking file",
			wantMoves: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			storage := filepath.Join(tmp, "storage")
			fsys := tt.fsSetup(t, storage)
			trk := tt.trkSetup(t, storage)
			fm := filemanager.New("repo", "host", fsys, trk)
			result, err := fm.AddMultiple(tt.paths)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %v, want containing %q", err, tt.errMsg)
				}
				if tt.wantMoves > 0 && len(fsys.moveCalls) != tt.wantMoves {
					t.Errorf("expected %d move calls, got %d: %v", tt.wantMoves, len(fsys.moveCalls), fsys.moveCalls)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.StagePaths) != tt.wantStages {
				t.Errorf("expected %d stage paths, got %d: %v", tt.wantStages, len(result.StagePaths), result.StagePaths)
			}
			if len(result.Rollback) != tt.wantRollbk {
				t.Errorf("expected %d rollback actions, got %d", tt.wantRollbk, len(result.Rollback))
			}
		})
	}
}

func TestManager_RollbackAll(t *testing.T) {
	t.Parallel()

	callOrder := []int{}
	actions := []func() error{
		func() error { callOrder = append(callOrder, 1); return nil },
		func() error { callOrder = append(callOrder, 2); return nil },
		func() error { callOrder = append(callOrder, 3); return nil },
	}

	fm := filemanager.New("repo", "host", &fakeFileSystem{}, &fakeTracker{})
	fm.RollbackAll(actions)

	want := []int{3, 2, 1}
	if len(callOrder) != len(want) {
		t.Fatalf("expected %d calls, got %d", len(want), len(callOrder))
	}
	for i := range want {
		if callOrder[i] != want[i] {
			t.Errorf("callOrder[%d] = %d, want %d", i, callOrder[i], want[i])
		}
	}
}

func TestManager_Remove(t *testing.T) {
	t.Parallel()

	t.Run("success_removes_symlink_and_returns_restore_function", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		repoPath := filepath.Join(tmp, "repo")
		os.MkdirAll(repoPath, 0o755)

		target := filepath.Join(tmp, "storage", "foo")
		os.MkdirAll(filepath.Dir(target), 0o755)
		os.WriteFile(target, []byte("hello"), 0o644)
		link := filepath.Join(tmp, "link")
		os.Symlink(target, link)

		fsys := &fakeFileSystem{
			validateSymlinkFunc: func(absPath, repoPath string) error { return nil },
			moveFunc: func(src, dst string, info os.FileInfo) error {
				return os.Rename(src, dst)
			},
		}
		trk := &fakeTracker{
			lnkFileNameFunc:       func() (string, error) { return ".lnk", nil },
			getManagedItemsFunc:   func() ([]string, error) { return []string{"link"}, nil },
			removeManagedItemFunc: func(path string) error { return nil },
		}

		fm := filemanager.New(repoPath, "host", fsys, trk)
		result, err := fm.Remove(filemanager.FileToTrack{AbsPath: link, RelativePath: "link"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.StagePaths) != 1 || result.StagePaths[0] != ".lnk" {
			t.Errorf("unexpected stage paths: %v", result.StagePaths)
		}
		if len(result.RemovePaths) != 1 || result.RemovePaths[0] != target {
			t.Errorf("unexpected remove paths: %v", result.RemovePaths)
		}

		// Symlink should be gone
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Error("expected symlink to be removed")
		}

		// Restore should move file back
		if result.RestoreFn == nil {
			t.Fatal("expected RestoreFn")
		}
		if err := result.RestoreFn(); err != nil {
			t.Fatalf("restore failed: %v", err)
		}
		if _, err := os.Stat(link); err != nil {
			t.Errorf("expected file to be restored at %s: %v", link, err)
		}
	})
}
