package lnkerror_test

import (
	"errors"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

func TestSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{"ErrGitRepoExists", lnkerror.ErrGitRepoExists},
		{"ErrAlreadyManaged", lnkerror.ErrAlreadyManaged},
		{"ErrNotManaged", lnkerror.ErrNotManaged},
		{"ErrNotInitialized", lnkerror.ErrNotInitialized},
		{"ErrNoPaths", lnkerror.ErrNoPaths},
		{"ErrDuplicatePath", lnkerror.ErrDuplicatePath},
		{"ErrNotInHome", lnkerror.ErrNotInHome},
		{"ErrNoChanges", lnkerror.ErrNoChanges},
		{"ErrDirtyTree", lnkerror.ErrDirtyTree},
		{"ErrDuplicateOwnership", lnkerror.ErrDuplicateOwnership},
		{"ErrInvalidFlags", lnkerror.ErrInvalidFlags},
		{"ErrBackupExists", lnkerror.ErrBackupExists},
		{"ErrBootstrapFailed", lnkerror.ErrBootstrapFailed},
		{"ErrBootstrapPerms", lnkerror.ErrBootstrapPerms},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err == nil {
				t.Fatalf("sentinel %s is nil", tt.name)
			}
		})
	}

	// Verify all sentinels are distinct.
	for i := 0; i < len(tests); i++ {
		for j := i + 1; j < len(tests); j++ {
			if errors.Is(tests[i].err, tests[j].err) {
				t.Fatalf("sentinel %s and %s are the same error", tests[i].name, tests[j].name)
			}
		}
	}
}

func TestError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *lnkerror.Error
		want string
	}{
		{
			name: "sentinel_only",
			err:  lnkerror.Wrap(lnkerror.ErrNotInitialized),
			want: "lnk repository not initialized",
		},
		{
			name: "with_path",
			err:  lnkerror.WithPath(lnkerror.ErrNotManaged, "foo.txt"),
			want: "file is not managed by lnk: foo.txt",
		},
		{
			name: "with_suggestion",
			err:  lnkerror.WithSuggestion(lnkerror.ErrBootstrapFailed, "check permissions"),
			want: "bootstrap script failed with error (check permissions)",
		},
		{
			name: "with_path_and_suggestion",
			err:  lnkerror.WithPathAndSuggestion(lnkerror.ErrNotInitialized, "repo", "run 'lnk init'"),
			want: "lnk repository not initialized: repo (run 'lnk init')",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	t.Parallel()

	wrapped := lnkerror.WithPath(lnkerror.ErrNotManaged, "foo.txt")

	if !errors.Is(wrapped, lnkerror.ErrNotManaged) {
		t.Error("expected errors.Is to match the underlying sentinel")
	}
	if errors.Is(wrapped, lnkerror.ErrAlreadyManaged) {
		t.Error("expected errors.Is to NOT match a different sentinel")
	}
}
