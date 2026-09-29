// internal/tracker/tracker_test.go
package tracker_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/tracker"
)

func TestTracker_LnkFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		format  tracker.RepoFormat
		want    string
		wantErr bool
	}{
		{"common_v2", "common", tracker.FormatV2, ".lnk.common", false},
		{"common_v1", "common", tracker.FormatV1, ".lnk", false},
		{"common_unknown", "common", tracker.FormatUnknown, "", true},
		{"host_v2", "myhost", tracker.FormatV2, ".lnk.myhost", false},
		{"host_v1", "myhost", tracker.FormatV1, ".lnk.myhost", false},
		{"host_unknown", "myhost", tracker.FormatUnknown, ".lnk.myhost", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := tracker.New("repo", tt.host, tt.format)
			got, err := tr.LnkFileName()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("LnkFileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTracker_HostStoragePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		format  tracker.RepoFormat
		want    string
		wantErr bool
	}{
		{"common_v2", "common", tracker.FormatV2, filepath.Join("REPO", "common.lnk"), false},
		{"common_v1", "common", tracker.FormatV1, "REPO", false},
		{"common_unknown", "common", tracker.FormatUnknown, "", true},
		{"host_v2", "myhost", tracker.FormatV2, filepath.Join("REPO", "myhost.lnk"), false},
		{"host_v1", "myhost", tracker.FormatV1, filepath.Join("REPO", "myhost.lnk"), false},
		{"host_unknown", "myhost", tracker.FormatUnknown, filepath.Join("REPO", "myhost.lnk"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := tracker.New("REPO", tt.host, tt.format)
			got, err := tr.HostStoragePath()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("HostStoragePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTracker_HostStorageRelPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		format  tracker.RepoFormat
		want    string
		wantErr bool
	}{
		{"common_v2", "common", tracker.FormatV2, "common.lnk", false},
		{"common_v1", "common", tracker.FormatV1, ".", false},
		{"common_unknown", "common", tracker.FormatUnknown, "", true},
		{"host_v2", "myhost", tracker.FormatV2, "myhost.lnk", false},
		{"host_v1", "myhost", tracker.FormatV1, "myhost.lnk", false},
		{"host_unknown", "myhost", tracker.FormatUnknown, "myhost.lnk", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := tracker.New("repo", tt.host, tt.format)
			got, err := tr.HostStorageRelPath()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("HostStorageRelPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTracker_GetManagedItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"file_does_not_exist", "__REMOVE__", nil},
		{"empty_file", "", []string{}},
		{"parses_lines_and_trims_whitespace", "foo\n\n  bar  \nbaz\n", []string{"foo", "bar", "baz"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			tr := tracker.New(tmp, "common", tracker.FormatV2)
			lnk := filepath.Join(tmp, ".lnk.common")
			if tt.content == "__REMOVE__" {
				os.Remove(lnk)
			} else {
				os.WriteFile(lnk, []byte(tt.content), 0o644)
			}

			items, err := tr.GetManagedItems()
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if len(items) != 0 {
					t.Errorf("expected empty, got %v", items)
				}
			} else if !slices.Equal(items, tt.want) {
				t.Errorf("got %v, want %v", items, tt.want)
			}
		})
	}
}

func TestTracker_AddManagedItem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seed    []string
		add     string
		want    []string
		wantErr bool
	}{
		{"adds_first_item", nil, "foo", []string{"foo"}, false},
		{"adds_second_item_and_sorts", []string{"foo"}, "baz", []string{"baz", "foo"}, false},
		{"duplicate_is_no_op", []string{"baz", "foo"}, "foo", []string{"baz", "foo"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			tr := tracker.New(tmp, "common", tracker.FormatV2)
			if tt.seed != nil {
				if err := tr.WriteManagedItems(tt.seed); err != nil {
					t.Fatal(err)
				}
			}
			if err := tr.AddManagedItem(tt.add); err != nil {
				if tt.wantErr {
					return
				}
				t.Fatal(err)
			}
			items, err := tr.GetManagedItems()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(items, tt.want) {
				t.Errorf("got %v, want %v", items, tt.want)
			}
		})
	}
}

func TestTracker_RemoveManagedItem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		seed   []string
		remove string
		want   []string
	}{
		{"removes_existing_item", []string{"alpha", "beta", "gamma"}, "beta", []string{"alpha", "gamma"}},
		{"nonexistent_item_is_no_op", []string{"alpha", "beta", "gamma"}, "delta", []string{"alpha", "beta", "gamma"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			tr := tracker.New(tmp, "common", tracker.FormatV2)
			if err := tr.WriteManagedItems(tt.seed); err != nil {
				t.Fatal(err)
			}
			if err := tr.RemoveManagedItem(tt.remove); err != nil {
				t.Fatal(err)
			}
			items, err := tr.GetManagedItems()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(items, tt.want) {
				t.Errorf("got %v, want %v", items, tt.want)
			}
		})
	}

	t.Run("removes_last_item", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		tr := tracker.New(tmp, "common", tracker.FormatV2)
		if err := tr.WriteManagedItems([]string{"alpha", "beta", "gamma"}); err != nil {
			t.Fatal(err)
		}
		if err := tr.RemoveManagedItem("alpha"); err != nil {
			t.Fatal(err)
		}
		if err := tr.RemoveManagedItem("gamma"); err != nil {
			t.Fatal(err)
		}
		if err := tr.RemoveManagedItem("beta"); err != nil {
			t.Fatal(err)
		}

		items, err := tr.GetManagedItems()
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 {
			t.Errorf("expected empty, got %v", items)
		}
		// Verify file is empty (0 bytes)
		lnk := filepath.Join(tmp, ".lnk.common")
		info, err := os.Stat(lnk)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 0 {
			t.Errorf("expected empty file, got %d bytes", info.Size())
		}
	})
}

func TestTracker_WriteManagedItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{"writes_items_with_trailing_newline", []string{"foo", "bar"}, "foo\nbar\n"},
		{"empty_slice_writes_empty_file", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			tr := tracker.New(tmp, "common", tracker.FormatV2)
			if err := tr.WriteManagedItems(tt.items); err != nil {
				t.Fatal(err)
			}

			lnk := filepath.Join(tmp, ".lnk.common")
			content, err := os.ReadFile(lnk)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != tt.want {
				t.Errorf("content = %q, want %q", string(content), tt.want)
			}
		})
	}
}
