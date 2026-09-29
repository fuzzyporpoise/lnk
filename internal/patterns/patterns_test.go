package patterns_test

import (
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/patterns"
)

func TestLoad_MissingFile(t *testing.T) {
	got, err := patterns.Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty patterns, got %v", got)
	}
}

func TestLoad_SkipsCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lnkinclude")
	content := "# comment\n\n*.md\n\n  # indented comment\n*.txt\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := patterns.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"*.md", "*.txt"}
	if len(got) != len(want) {
		t.Fatalf("expected %d patterns, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pattern[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMatch_BasicInclude(t *testing.T) {
	included, err := patterns.Match([]string{"*.md"}, "readme.md")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !included {
		t.Error("expected readme.md to match *.md")
	}
}

func TestMatch_BasicExclude(t *testing.T) {
	included, err := patterns.Match([]string{"*.md"}, "readme.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if included {
		t.Error("expected readme.txt not to match *.md")
	}
}

func TestMatch_Negation(t *testing.T) {
	included, err := patterns.Match([]string{"*.md", "!readme.md"}, "readme.md")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if included {
		t.Error("expected readme.md to be excluded by negation")
	}
}

func TestMatch_LastMatchWins(t *testing.T) {
	included, err := patterns.Match([]string{"!*.md", "*.md"}, "readme.md")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !included {
		t.Error("expected last pattern *.md to win")
	}
}

func TestMatch_DirectoryPattern(t *testing.T) {
	included, err := patterns.Match([]string{"build/"}, "build/output.js")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !included {
		t.Error("expected build/output.js to match build/")
	}
}

func TestMatch_DoubleGlob(t *testing.T) {
	included, err := patterns.Match([]string{"**/*.md"}, "docs/guides/readme.md")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !included {
		t.Error("expected docs/guides/readme.md to match **/*.md")
	}
}

func TestMatch_SingleWildcard(t *testing.T) {
	included, err := patterns.Match([]string{"file?.txt"}, "file1.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !included {
		t.Error("expected file1.txt to match file?.txt")
	}

	included, err = patterns.Match([]string{"file?.txt"}, "file10.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if included {
		t.Error("expected file10.txt not to match file?.txt")
	}

	included, err = patterns.Match([]string{"file?.txt"}, "file.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if included {
		t.Error("expected file.txt not to match file?.txt")
	}
}

func TestMatch_CharacterClass(t *testing.T) {
	included, err := patterns.Match([]string{"file[0-9].txt"}, "file1.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !included {
		t.Error("expected file1.txt to match file[0-9].txt")
	}

	included, err = patterns.Match([]string{"file[0-9].txt"}, "filea.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if included {
		t.Error("expected filea.txt not to match file[0-9].txt")
	}
}

func TestMatch_EmptyPatterns(t *testing.T) {
	included, err := patterns.Match([]string{}, "anything.txt")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if included {
		t.Error("expected empty patterns to exclude everything")
	}
}
