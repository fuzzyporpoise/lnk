// internal/fs/filesystem_test.go
package fs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/fs"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

func TestFileSystem_ValidateFileInfoForAdd(t *testing.T) {
	t.Parallel()

	var fsys fs.FileSystem

	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr error
		isDir   bool
	}{
		{
			name: "file_not_found",
			setup: func(t *testing.T) string {
				return "/nonexistent/path"
			},
			wantErr: fs.ErrFileNotExists,
		},
		{
			name: "regular_file",
			setup: func(t *testing.T) string {
				tmp := t.TempDir()
				path := filepath.Join(tmp, "file.txt")
				os.WriteFile(path, []byte("hello"), 0o644)
				return path
			},
			wantErr: nil,
		},
		{
			name: "directory",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: nil,
			isDir:   true,
		},
		{
			name: "symlink_rejected",
			setup: func(t *testing.T) string {
				tmp := t.TempDir()
				target := filepath.Join(tmp, "target")
				link := filepath.Join(tmp, "link")
				os.WriteFile(target, []byte("x"), 0o644)
				os.Symlink(target, link)
				return link
			},
			wantErr: fs.ErrUnsupportedType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := tt.setup(t)
			info, err := fsys.ValidateFileInfoForAdd(path)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.isDir && !info.IsDir() {
				t.Error("expected directory")
			}
		})
	}
}

func TestFileSystem_ValidateSymlinkForRemove(t *testing.T) {
	t.Parallel()

	var fsys fs.FileSystem

	tests := []struct {
		name    string
		setup   func(t *testing.T) (path, repo string)
		wantErr error
	}{
		{
			name: "not_a_symlink",
			setup: func(t *testing.T) (string, string) {
				tmp := t.TempDir()
				path := filepath.Join(tmp, "file.txt")
				os.WriteFile(path, []byte("x"), 0o644)
				return path, tmp
			},
			wantErr: lnkerror.ErrNotManaged,
		},
		{
			name: "nonexistent_path",
			setup: func(t *testing.T) (string, string) {
				return "/nonexistent", "/repo"
			},
			wantErr: fs.ErrFileNotExists,
		},
		{
			name: "symlink_outside_repo",
			setup: func(t *testing.T) (string, string) {
				tmp := t.TempDir()
				repo := filepath.Join(tmp, "repo")
				os.MkdirAll(repo, 0o755)
				outside := filepath.Join(tmp, "outside")
				link := filepath.Join(tmp, "link")
				os.WriteFile(outside, []byte("x"), 0o644)
				os.Symlink(outside, link)
				return link, repo
			},
			wantErr: lnkerror.ErrNotManaged,
		},
		{
			name: "symlink_inside_repo",
			setup: func(t *testing.T) (string, string) {
				tmp := t.TempDir()
				repo := filepath.Join(tmp, "repo")
				storage := filepath.Join(repo, "storage")
				os.MkdirAll(storage, 0o755)
				target := filepath.Join(storage, "file.txt")
				link := filepath.Join(tmp, "link")
				os.WriteFile(target, []byte("x"), 0o644)
				os.Symlink(target, link)
				return link, repo
			},
			wantErr: nil,
		},
		{
			name: "relative_symlink_inside_repo",
			setup: func(t *testing.T) (string, string) {
				tmp := t.TempDir()
				repo := filepath.Join(tmp, "repo")
				storage := filepath.Join(repo, "storage")
				os.MkdirAll(storage, 0o755)
				target := filepath.Join(storage, "file.txt")
				link := filepath.Join(repo, "link.txt")
				os.WriteFile(target, []byte("x"), 0o644)
				rel, _ := filepath.Rel(repo, target)
				os.Symlink(rel, link)
				return link, repo
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path, repo := tt.setup(t)
			err := fsys.ValidateSymlinkForRemove(path, repo)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFileSystem_MoveFile(t *testing.T) {
	t.Parallel()

	var fsys fs.FileSystem

	t.Run("moves_file_and_creates_parent_dirs", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		src := filepath.Join(tmp, "src.txt")
		dst := filepath.Join(tmp, "nested", "dst.txt")
		os.WriteFile(src, []byte("hello"), 0o644)

		err := fsys.MoveFile(src, dst)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Error("expected src to not exist")
		}
		content, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "hello" {
			t.Errorf("unexpected content: %s", content)
		}
	})
}

func TestFileSystem_MoveDirectory(t *testing.T) {
	t.Parallel()

	var fsys fs.FileSystem

	t.Run("moves_directory_with_contents", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		src := filepath.Join(tmp, "src")
		dst := filepath.Join(tmp, "moved", "dst")
		os.MkdirAll(filepath.Join(src, "subdir"), 0o755)
		os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o644)

		err := fsys.MoveDirectory(src, dst)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Error("expected src to not exist")
		}
		if _, err := os.Stat(filepath.Join(dst, "file.txt")); err != nil {
			t.Errorf("expected file in dst: %v", err)
		}
	})
}

func TestFileSystem_Move(t *testing.T) {
	t.Parallel()

	var fsys fs.FileSystem

	tests := []struct {
		name  string
		setup func(t *testing.T) (src, dst string, info os.FileInfo)
	}{
		{
			name: "delegates_to_MoveFile_for_files",
			setup: func(t *testing.T) (string, string, os.FileInfo) {
				tmp := t.TempDir()
				src := filepath.Join(tmp, "file.txt")
				dst := filepath.Join(tmp, "moved.txt")
				os.WriteFile(src, []byte("x"), 0o644)
				info, _ := os.Stat(src)
				return src, dst, info
			},
		},
		{
			name: "delegates_to_MoveDirectory_for_dirs",
			setup: func(t *testing.T) (string, string, os.FileInfo) {
				tmp := t.TempDir()
				src := filepath.Join(tmp, "dir")
				dst := filepath.Join(tmp, "moved", "dir")
				os.MkdirAll(src, 0o755)
				info, _ := os.Stat(src)
				return src, dst, info
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src, dst, info := tt.setup(t)
			err := fsys.Move(src, dst, info)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dst); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFileSystem_CreateSymlink(t *testing.T) {
	t.Parallel()

	var fsys fs.FileSystem

	t.Run("creates_relative_symlink", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		target := filepath.Join(tmp, "storage", "file.txt")
		link := filepath.Join(tmp, "link.txt")
		os.MkdirAll(filepath.Dir(target), 0o755)
		os.WriteFile(target, []byte("x"), 0o644)

		err := fsys.CreateSymlink(target, link)
		if err != nil {
			t.Fatal(err)
		}

		got, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.IsAbs(got) {
			t.Errorf("expected relative symlink, got absolute: %s", got)
		}
		// filepath.Rel behavior varies by platform
		want := filepath.Join("storage", "file.txt")
		if got != want {
			t.Errorf("symlink target = %q, want %q", got, want)
		}
	})
}

func TestRemoveEmptyDirs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T) (root string, removed []string, kept []string)
	}{
		{
			name: "removes_nested_empty_directories",
			setup: func(t *testing.T) (string, []string, []string) {
				tmp := t.TempDir()
				empty1 := filepath.Join(tmp, "a", "b", "c")
				empty2 := filepath.Join(tmp, "a", "b")
				keep := filepath.Join(tmp, "a", "keep")
				os.MkdirAll(empty1, 0o755)
				os.MkdirAll(keep, 0o755)
				os.WriteFile(filepath.Join(keep, "file.txt"), []byte("x"), 0o644)
				return tmp, []string{empty1, empty2}, []string{keep}
			},
		},
		{
			name: "keeps_non_empty_root",
			setup: func(t *testing.T) (string, []string, []string) {
				tmp := t.TempDir()
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("x"), 0o644)
				return tmp, nil, []string{tmp}
			},
		},
		{
			name: "removes_sibling_empty_dirs",
			setup: func(t *testing.T) (string, []string, []string) {
				tmp := t.TempDir()
				emptyA := filepath.Join(tmp, "emptyA")
				emptyB := filepath.Join(tmp, "emptyB")
				full := filepath.Join(tmp, "full")
				os.MkdirAll(emptyA, 0o755)
				os.MkdirAll(emptyB, 0o755)
				os.MkdirAll(full, 0o755)
				os.WriteFile(filepath.Join(full, "f.txt"), []byte("x"), 0o644)
				return tmp, []string{emptyA, emptyB}, []string{full}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, removed, kept := tt.setup(t)
			err := fs.RemoveEmptyDirs(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range removed {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("expected %q to be removed", p)
				}
			}
			for _, p := range kept {
				if _, err := os.Stat(p); err != nil {
					t.Errorf("expected %q to exist: %v", p, err)
				}
			}
		})
	}
}
