// internal/git/git_test.go
package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/git"
	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// ---------- helpers ----------

func initRepo(t *testing.T, path string) *git.Git {
	t.Helper()
	g := git.New(path)
	if err := g.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return g
}

// configureGit sets an ambient identity in the repo's local config, mimicking
// a user who has configured git themselves.
func configureGit(t *testing.T, g *git.Git) {
	t.Helper()
	for key, value := range map[string]string{
		"user.name":  "Test User",
		"user.email": "test@example.com",
	} {
		if _, err := g.Run(context.Background(), "config", key, value); err != nil {
			t.Fatalf("configure %s: %v", key, err)
		}
	}
}

// isolateGitEnv points git at empty config files so no ambient identity leaks
// in from the developer's machine or CI, and unsets the identity environment
// variables for the same reason. It returns the temp home directory.
// It cannot be used from a parallel test because it mutates the process env.
func isolateGitEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(home, ".gitconfig-system"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Git rejects a set-but-empty identity variable instead of ignoring it, so
	// these must be truly unset rather than emptied.
	for _, key := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
		"EMAIL",
	} {
		unsetEnv(t, key)
	}
	return home
}

// unsetEnv removes key from the process environment for the duration of the
// test, restoring its prior value (or absence) on cleanup.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, wasSet := os.LookupEnv(key)
	t.Cleanup(func() {
		if wasSet {
			os.Setenv(key, value)
		} else {
			os.Unsetenv(key)
		}
	})
	os.Unsetenv(key)
}

// commitAuthor returns the "Name <email>" of the most recent commit.
func commitAuthor(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "log", "-1", "--format=%an <%ae>").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// configHasUserIdentity reports whether the repo's local config carries a
// user.name, which lnk must never write.
func configHasUserIdentity(t *testing.T, repoPath string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "config", "--local", "--get", "user.name").CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// ---------- tests ----------

func TestGit_Init(t *testing.T) {
	t.Parallel()

	t.Run("initializes_git_repository", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := git.New(tmp)

		if g.IsGitRepository() {
			t.Error("expected not a git repo before init")
		}

		if err := g.Init(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !g.IsGitRepository() {
			t.Error("expected git repo after init")
		}
	})
}

func TestGit_Run(t *testing.T) {
	t.Parallel()

	t.Run("executes_read_only_command", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := initRepo(t, tmp)

		out, err := g.Run(context.Background(), "rev-parse", "--is-inside-work-tree")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if strings.TrimSpace(string(out)) != "true" {
			t.Errorf("output = %q, want true", out)
		}
	})

	t.Run("returns_error_output", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := initRepo(t, tmp)

		if _, err := g.Run(context.Background(), "not-a-command"); err == nil {
			t.Fatal("expected error for unknown command")
		}
	})
}

func TestGit_CommitIdentity(t *testing.T) {
	t.Run("CommitAs_authors_lnk_identity_without_writing_config", func(t *testing.T) {
		isolateGitEnv(t)
		tmp := t.TempDir()
		g := initRepo(t, tmp)
		if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := g.CommitAs(context.Background(), git.LnkCommitName, git.LnkCommitEmail, "machine"); err != nil {
			t.Fatalf("CommitAs: %v", err)
		}

		if got, want := commitAuthor(t, tmp), "Lnk User <lnk@localhost>"; got != want {
			t.Errorf("author = %q, want %q", got, want)
		}
		if configHasUserIdentity(t, tmp) {
			t.Error("CommitAs must not write a user identity to .git/config")
		}
	})

	t.Run("Commit_uses_ambient_identity", func(t *testing.T) {
		isolateGitEnv(t)
		tmp := t.TempDir()
		g := initRepo(t, tmp)
		configureGit(t, g)
		if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := g.Commit(context.Background(), "mine"); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		if got, want := commitAuthor(t, tmp), "Test User <test@example.com>"; got != want {
			t.Errorf("author = %q, want %q", got, want)
		}
	})

	t.Run("Commit_falls_back_without_identity", func(t *testing.T) {
		isolateGitEnv(t)
		tmp := t.TempDir()
		g := initRepo(t, tmp)
		if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := g.Commit(context.Background(), "auto"); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		if got, want := commitAuthor(t, tmp), "Lnk User <lnk@localhost>"; got != want {
			t.Errorf("author = %q, want %q", got, want)
		}
		if configHasUserIdentity(t, tmp) {
			t.Error("Commit fallback must not write a user identity to .git/config")
		}
	})

	t.Run("Commit_uses_env_identity_without_config", func(t *testing.T) {
		isolateGitEnv(t)
		t.Setenv("GIT_AUTHOR_NAME", "Env User")
		t.Setenv("GIT_AUTHOR_EMAIL", "env@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Env User")
		t.Setenv("GIT_COMMITTER_EMAIL", "env@example.com")
		tmp := t.TempDir()
		g := initRepo(t, tmp)
		if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := g.Commit(context.Background(), "env"); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		if got, want := commitAuthor(t, tmp), "Env User <env@example.com>"; got != want {
			t.Errorf("author = %q, want %q (env identity must not trigger the lnk fallback)", got, want)
		}
	})

	t.Run("CommitAs_scopes_commit_to_paths", func(t *testing.T) {
		isolateGitEnv(t)
		tmp := t.TempDir()
		g := initRepo(t, tmp)
		for name, content := range map[string]string{"keep.txt": "keep", "other.txt": "other"} {
			if err := os.WriteFile(filepath.Join(tmp, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := g.CommitAs(context.Background(), git.LnkCommitName, git.LnkCommitEmail, "scoped", "keep.txt"); err != nil {
			t.Fatalf("CommitAs: %v", err)
		}

		// other.txt was staged but sits outside the pathspec: it must stay
		// staged and must not reach HEAD.
		out, err := exec.Command("git", "-C", tmp, "status", "--porcelain").CombinedOutput()
		if err != nil {
			t.Fatalf("git status: %v\n%s", err, out)
		}
		if got, want := strings.TrimSpace(string(out)), "A  other.txt"; got != want {
			t.Errorf("status = %q, want %q (staged and uncommitted)", got, want)
		}
		if out, err := exec.Command("git", "-C", tmp, "cat-file", "-e", "HEAD:other.txt").CombinedOutput(); err == nil {
			t.Errorf("other.txt reached HEAD despite the pathspec: %s", out)
		}
	})
}

func TestGit_LocalIdentity(t *testing.T) {
	isolateGitEnv(t)
	tmp := t.TempDir()
	g := initRepo(t, tmp)

	name, email, err := g.LocalIdentity(context.Background())
	if err != nil {
		t.Fatalf("LocalIdentity: %v", err)
	}
	if name != "" || email != "" {
		t.Errorf("expected empty local identity, got %q %q", name, email)
	}

	configureGit(t, g)
	name, email, err = g.LocalIdentity(context.Background())
	if err != nil {
		t.Fatalf("LocalIdentity: %v", err)
	}
	if name != "Test User" || email != "test@example.com" {
		t.Errorf("local identity = %q %q, want %q %q", name, email, "Test User", "test@example.com")
	}

	if err := g.UnsetLocalIdentity(context.Background()); err != nil {
		t.Fatalf("UnsetLocalIdentity: %v", err)
	}
	// Idempotent: unsetting already-absent keys must not error.
	if err := g.UnsetLocalIdentity(context.Background()); err != nil {
		t.Fatalf("UnsetLocalIdentity (second call): %v", err)
	}

	name, email, err = g.LocalIdentity(context.Background())
	if err != nil {
		t.Fatalf("LocalIdentity: %v", err)
	}
	if name != "" || email != "" {
		t.Errorf("expected cleared local identity, got %q %q", name, email)
	}
}

func TestGit_Commit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, tmp string, g *git.Git)
		wantErr bool
		errMsg  string
	}{
		{
			name: "commits_staged_changes",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				configureGit(t, g)
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)
				if err := g.AddAll(context.Background()); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: false,
		},
		{
			name: "fails_without_staged_changes",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				configureGit(t, g)
			},
			wantErr: true,
			errMsg:  "git operation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			g := initRepo(t, tmp)
			tt.setup(t, tmp, g)

			err := g.Commit(context.Background(), "test commit")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGit_HasChanges(t *testing.T) {
	t.Parallel()

	t.Run("detects_dirty_and_clean_states", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := initRepo(t, tmp)

		dirty, err := g.HasChanges(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if dirty {
			t.Error("expected clean repo")
		}

		os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)

		dirty, err = g.HasChanges(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !dirty {
			t.Error("expected dirty repo")
		}
	})
}

func TestGit_Diff(t *testing.T) {
	t.Parallel()

	t.Run("returns_diff_for_uncommitted_changes", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := initRepo(t, tmp)
		configureGit(t, g)

		// Create and commit a tracked file
		os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("original"), 0o644)
		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := g.Commit(context.Background(), "initial"); err != nil {
			t.Fatal(err)
		}

		// Modify the tracked file
		os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)

		diff, err := g.Diff(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(diff, "hello") {
			t.Errorf("expected diff to contain content, got: %s", diff)
		}
	})
}

func TestGit_AddAll(t *testing.T) {
	t.Parallel()

	t.Run("stages_new_files", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := initRepo(t, tmp)

		os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)

		if err := g.AddAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		// Staged changes are still "dirty" until committed
		dirty, err := g.HasChanges(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !dirty {
			t.Error("expected staged changes to show as dirty")
		}
	})
}

func TestGit_GetStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(t *testing.T) (g *git.Git, cleanup func())
		wantAhead  int
		wantBehind int
		wantRemote string
		wantDirty  bool
	}{
		{
			name: "local_only_status_without_remote",
			setup: func(t *testing.T) (*git.Git, func()) {
				tmp := t.TempDir()
				g := initRepo(t, tmp)
				configureGit(t, g)
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)
				if err := g.AddAll(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := g.Commit(context.Background(), "initial"); err != nil {
					t.Fatal(err)
				}
				return g, func() {}
			},
			wantAhead:  1,
			wantBehind: 0,
			wantRemote: "",
			wantDirty:  false,
		},
		{
			name: "dirty_working_tree",
			setup: func(t *testing.T) (*git.Git, func()) {
				tmp := t.TempDir()
				g := initRepo(t, tmp)
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)
				return g, func() {}
			},
			wantAhead:  0,
			wantBehind: 0,
			wantRemote: "",
			wantDirty:  true,
		},
		{
			name: "status_with_remote",
			setup: func(t *testing.T) (*git.Git, func()) {
				remote := testhelpers.NewBareRemote(t)
				_ = testhelpers.PushInitialCommit(t, remote)

				dst := filepath.Join(t.TempDir(), "clone")
				g := git.New(dst)
				if err := g.Clone(context.Background(), remote); err != nil {
					t.Fatalf("Clone: %v", err)
				}
				return g, func() {}
			},
			wantAhead:  0,
			wantBehind: 0,
			wantRemote: "origin/main",
			wantDirty:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, cleanup := tt.setup(t)
			defer cleanup()

			status, err := g.GetStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.Ahead != tt.wantAhead {
				t.Errorf("expected Ahead=%d, got %d", tt.wantAhead, status.Ahead)
			}
			if status.Behind != tt.wantBehind {
				t.Errorf("expected Behind=%d, got %d", tt.wantBehind, status.Behind)
			}
			if status.Remote != tt.wantRemote {
				t.Errorf("expected Remote=%q, got %q", tt.wantRemote, status.Remote)
			}
			if status.Dirty != tt.wantDirty {
				t.Errorf("expected Dirty=%v, got %v", tt.wantDirty, status.Dirty)
			}
		})
	}
}

func TestGit_Push(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) (g *git.Git, remote string)
		wantErr error
	}{
		{
			name: "fails_without_remote",
			setup: func(t *testing.T) (*git.Git, string) {
				tmp := t.TempDir()
				return initRepo(t, tmp), ""
			},
			wantErr: git.ErrPush,
		},
		{
			name: "pushes_to_remote",
			setup: func(t *testing.T) (*git.Git, string) {
				remote := testhelpers.NewBareRemote(t)
				src := testhelpers.PushInitialCommit(t, remote)

				// Add another commit and push
				g := git.New(src)
				configureGit(t, g)
				os.WriteFile(filepath.Join(src, "file2.txt"), []byte("world"), 0o644)
				if err := g.AddAll(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := g.Commit(context.Background(), "second"); err != nil {
					t.Fatal(err)
				}
				return g, remote
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, _ := tt.setup(t)
			err := g.Push(context.Background())
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
				t.Fatalf("Push: %v", err)
			}
		})
	}
}

func TestGit_Pull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) (g *git.Git, remote string)
		wantErr error
	}{
		{
			name: "fails_without_remote",
			setup: func(t *testing.T) (*git.Git, string) {
				tmp := t.TempDir()
				return initRepo(t, tmp), ""
			},
			wantErr: git.ErrPull,
		},
		{
			name: "pulls_from_remote",
			setup: func(t *testing.T) (*git.Git, string) {
				remote := testhelpers.NewBareRemote(t)
				src := testhelpers.PushInitialCommit(t, remote)

				// Clone into dst
				dst := filepath.Join(t.TempDir(), "clone")
				g := git.New(dst)
				if err := g.Clone(context.Background(), remote); err != nil {
					t.Fatalf("Clone: %v", err)
				}

				// Add commit to source and push
				gSrc := git.New(src)
				configureGit(t, gSrc)
				os.WriteFile(filepath.Join(src, "pulled.txt"), []byte("new"), 0o644)
				if err := gSrc.AddAll(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := gSrc.Commit(context.Background(), "add pulled"); err != nil {
					t.Fatal(err)
				}
				if err := gSrc.Push(context.Background()); err != nil {
					t.Fatal(err)
				}

				return g, remote
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, _ := tt.setup(t)
			err := g.Pull(context.Background())
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
				t.Fatalf("Pull: %v", err)
			}
		})
	}
}

func TestGit_Clone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T) (remote, dst string)
	}{
		{
			name: "clones_from_bare_remote",
			setup: func(t *testing.T) (string, string) {
				remote := testhelpers.NewBareRemote(t)
				_ = testhelpers.PushInitialCommit(t, remote)
				dst := filepath.Join(t.TempDir(), "clone")
				return remote, dst
			},
		},
		{
			name: "overwrites_existing_directory",
			setup: func(t *testing.T) (string, string) {
				remote := testhelpers.NewBareRemote(t)
				_ = testhelpers.PushInitialCommit(t, remote)
				dst := filepath.Join(t.TempDir(), "clone")
				os.MkdirAll(dst, 0o755)
				os.WriteFile(filepath.Join(dst, "old.txt"), []byte("old"), 0o644)
				return remote, dst
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			remote, dst := tt.setup(t)
			g := git.New(dst)
			if err := g.Clone(context.Background(), remote); err != nil {
				t.Fatalf("Clone: %v", err)
			}
			if !g.IsGitRepository() {
				t.Error("expected cloned directory to be a git repo")
			}
			if _, err := os.Stat(filepath.Join(dst, ".lnkrepo")); err != nil {
				t.Errorf("expected .lnkrepo in clone: %v", err)
			}
			if tt.name == "overwrites_existing_directory" {
				if _, err := os.Stat(filepath.Join(dst, "old.txt")); !os.IsNotExist(err) {
					t.Error("expected old file to be removed")
				}
			}
		})
	}
}

func TestGit_Stage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T, tmp string, g *git.Git)
		wantDirty bool
	}{
		{
			name: "stages_existing_file",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				configureGit(t, g)
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)
				if err := g.Stage(context.Background(), "file.txt"); err != nil {
					t.Fatalf("Stage: %v", err)
				}
			},
			wantDirty: true,
		},
		{
			name: "stages_deletion_of_removed_file",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				configureGit(t, g)
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)
				if err := g.AddAll(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := g.Commit(context.Background(), "initial"); err != nil {
					t.Fatal(err)
				}
				os.Remove(filepath.Join(tmp, "file.txt"))
				if err := g.Stage(context.Background(), "file.txt"); err != nil {
					t.Fatalf("Stage: %v", err)
				}
			},
			wantDirty: true,
		},
		{
			name: "ignores_untracked_missing_file",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				if err := g.Stage(context.Background(), "nonexistent.txt"); err != nil {
					t.Fatalf("Stage: %v", err)
				}
			},
			wantDirty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			g := initRepo(t, tmp)
			tt.setup(t, tmp, g)

			dirty, err := g.HasChanges(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if dirty != tt.wantDirty {
				t.Errorf("expected dirty=%v, got %v", tt.wantDirty, dirty)
			}
		})
	}
}

func TestGit_Options(t *testing.T) {
	t.Parallel()

	t.Run("WithColor_adds_color_flag", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		g := git.New(tmp, git.WithColor())
		_ = g.Init(context.Background())
	})

}

func TestGit_HasStagedChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(t *testing.T, tmp string, g *git.Git)
		wantStaged bool
	}{
		{
			name: "detects_staged_changes",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				configureGit(t, g)
				os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)
				_ = g.AddAll(context.Background())
			},
			wantStaged: true,
		},
		{
			name: "no_staged_changes",
			setup: func(t *testing.T, tmp string, g *git.Git) {
				configureGit(t, g)
			},
			wantStaged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			g := initRepo(t, tmp)
			tt.setup(t, tmp, g)

			staged, err := g.HasStagedChanges(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if staged != tt.wantStaged {
				t.Errorf("expected staged=%v, got %v", tt.wantStaged, staged)
			}
		})
	}
}
