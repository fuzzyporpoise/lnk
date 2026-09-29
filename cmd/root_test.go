package cmd_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/cmd"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

func TestPrintRestore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		info   service.RestoreInfo
		dryRun bool
		want   []string
		unwant []string
	}{
		{
			name: "live_restore_without_backups",
			info: service.RestoreInfo{
				Restored: []string{"a", "b"},
			},
			dryRun: false,
			want:   []string{"Restored 2 path(s)", "  a\n", "  b\n"},
			unwant: []string{"Backed up"},
		},
		{
			name: "dry_run_with_backups",
			info: service.RestoreInfo{
				Restored: []string{"a"},
				BackedUp: []string{"b", "c"},
			},
			dryRun: true,
			want:   []string{"Would restore 1 path(s)", "Would back up 2 conflicting path(s)", "  b\n", "  c\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := cmd.PrintRestore(&buf, tt.info, tt.dryRun); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("expected output to contain %q, got:\n%s", w, out)
				}
			}
			for _, u := range tt.unwant {
				if strings.Contains(out, u) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", u, out)
				}
			}
		})
	}
}

func TestPrintDoctor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report service.DoctorReport
		want   []string
	}{
		{
			name:   "minimal_report",
			report: service.DoctorReport{Mode: "check"},
			want:   []string{"Mode: check"},
		},
		{
			name:   "marker_missing",
			report: service.DoctorReport{Mode: "check", MarkerMissing: true},
			want:   []string{"Repo marker missing"},
		},
		{
			name:   "marker_fixed",
			report: service.DoctorReport{Mode: "fix", MarkerMissing: true, MarkerFixed: true},
			want:   []string{"Repo marker was added"},
		},
		{
			name: "collisions",
			report: service.DoctorReport{
				Mode: "check",
				Collisions: []service.OwnershipCollision{
					{Path: "foo", Scopes: []string{"common", "host1"}},
				},
			},
			want: []string{"Ownership collisions:", "  foo -> common, host1"},
		},
		{
			name:   "empty_scopes",
			report: service.DoctorReport{Mode: "check", EmptyScopes: []string{"host1", "host2"}},
			want:   []string{"Empty host scopes:"},
		},
		{
			name:   "pruned_scopes",
			report: service.DoctorReport{Mode: "fix", PrunedScopes: []string{"host1"}},
			want:   []string{"Pruned empty host scopes:"},
		},
		{
			name:   "broken_symlink_skipped",
			report: service.DoctorReport{Mode: "check", BrokenSymlinkFixSkipped: true},
			want:   []string{"Broken symlink repair was skipped"},
		},
		{
			name:   "broken_symlink_fixed",
			report: service.DoctorReport{Mode: "fix", BrokenSymlinkFix: true},
			want:   []string{"Broken symlinks repaired"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := cmd.PrintDoctor(&buf, tt.report); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("expected output to contain %q, got:\n%s", w, out)
				}
			}
		})
	}
}

func TestNewRootCommand(t *testing.T) {
	// Note: Not parallel due to shared package-level version state.

	t.Run("version_includes_build_info", func(t *testing.T) {
		cmd.SetVersion("1.0.0", "2024-01-01")
		root := cmd.NewRootCommand()
		if !strings.Contains(root.Version, "1.0.0") {
			t.Errorf("version missing tag: %s", root.Version)
		}
		if !strings.Contains(root.Version, "2024-01-01") {
			t.Errorf("version missing build time: %s", root.Version)
		}
	})

	t.Run("all_subcommands_registered", func(t *testing.T) {
		root := cmd.NewRootCommand()
		want := []string{
			"init", "clone", "add", "create", "move", "remove", "forget",
			"list", "status", "diff", "commit", "push", "pull",
			"restore", "update", "doctor", "format", "bootstrap",
			"project",
		}
		for _, name := range want {
			c, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatalf("Find %q: %v", name, err)
			}
			if c == root {
				t.Errorf("subcommand %q not found", name)
			}
		}
	})

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "move_flags_are_mutually_exclusive",
			args: []string{"move", "foo", "--to-common", "--to-host", "h"},
		},
		{
			name: "list_flags_are_mutually_exclusive",
			args: []string{"list", "--all", "--host", "h"},
		},
		{
			name: "doctor_flags_are_mutually_exclusive",
			args: []string{"doctor", "--all", "--host", "h"},
		},
		{
			name: "format_flags_are_mutually_exclusive",
			args: []string{"format", "--v1", "--v2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCommand()
			root.SetArgs(tt.args)
			err := root.Execute()
			if err == nil {
				t.Fatal("expected error for mutually exclusive flags")
			}
			msg := err.Error()
			if !strings.Contains(msg, "mutually exclusive") && !strings.Contains(msg, "none of the others") {
				t.Errorf("unexpected error message: %s", msg)
			}
		})
	}
}

func TestInitCommand(t *testing.T) {
	t.Run("initializes_repo", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", "")

		repoPath := filepath.Join(home, ".config", "lnk")

		var buf bytes.Buffer
		root := cmd.NewRootCommand()
		root.SetOut(&buf)
		root.SetErr(&buf)
		root.SetArgs([]string{"--repo", repoPath, "init"})

		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v\noutput: %s", err, buf.String())
		}

		if !strings.Contains(buf.String(), "Initialized repo at") {
			t.Errorf("unexpected output: %s", buf.String())
		}
	})
}
