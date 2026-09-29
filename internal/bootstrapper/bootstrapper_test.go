// internal/bootstrapper/bootstrapper_test.go
package bootstrapper_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/bootstrapper"
	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
)

// --- stubs ---

type fakeGit struct {
	isRepo bool
}

func (f *fakeGit) IsGitRepository() bool { return f.isRepo }

// --- tests ---

func TestRunner_FindScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		isRepo  bool
		write   string // script content to write, empty = none
		want    string
		wantErr error
	}{
		{
			name:    "returns_error_when_not_a_git_repo",
			isRepo:  false,
			wantErr: lnkerror.ErrNotInitialized,
		},
		{
			name:   "finds_bootstrap_sh",
			isRepo: true,
			write:  "#!/bin/bash\necho ok",
			want:   "bootstrap.sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			if tt.write != "" {
				script := filepath.Join(tmp, "bootstrap.sh")
				if err := os.WriteFile(script, []byte(tt.write), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			r := bootstrapper.New(tmp, &fakeGit{isRepo: tt.isRepo})
			name, err := r.FindScript()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, name)
			}
		})
	}
}

func TestRunner_RunScript(t *testing.T) {
	t.Parallel()

	t.Run("executes_script_successfully", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		script := filepath.Join(tmp, "bootstrap.sh")
		os.WriteFile(script, []byte("#!/bin/bash\necho hello"), 0o755)

		r := bootstrapper.New(tmp, &fakeGit{isRepo: true})

		var stdout bytes.Buffer
		err := r.RunScript(context.Background(), "bootstrap.sh", &stdout, io.Discard, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "hello") {
			t.Fatalf("expected output to contain hello, got %s", stdout.String())
		}
	})
}
