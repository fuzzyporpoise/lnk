package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

func TestProjectCache_LoadSaveAndGetSetRemove(t *testing.T) {
	svc, _ := testhelpers.TestHome(t)
	ps := service.NewProjectService(svc)

	cache, err := ps.LoadProjectCache()
	if err != nil {
		t.Fatalf("LoadProjectCache: %v", err)
	}
	if len(cache.Projects) != 0 {
		t.Errorf("new cache has %d entries, want 0", len(cache.Projects))
	}

	cache.Set(service.ProjectCacheEntry{ID: "github.com/user/repo", Path: "/tmp/repo", State: service.CacheStateAvailable})
	if err := ps.SaveProjectCache(cache); err != nil {
		t.Fatalf("SaveProjectCache: %v", err)
	}

	loaded, err := ps.LoadProjectCache()
	if err != nil {
		t.Fatalf("LoadProjectCache after save: %v", err)
	}
	entry, ok := loaded.Get("github.com/user/repo")
	if !ok {
		t.Fatal("expected cached entry")
	}
	if entry.Path != "/tmp/repo" || entry.State != service.CacheStateAvailable {
		t.Errorf("entry = %+v, want available /tmp/repo", entry)
	}

	cache.Remove("github.com/user/repo")
	if _, ok := cache.Get("github.com/user/repo"); ok {
		t.Error("expected entry to be removed")
	}
}

func clearProjectCache(t *testing.T, svc *service.Service) {
	t.Helper()
	path := filepath.Join(svc.RepoPath(), ".lnkprojectcache")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clear project cache: %v", err)
	}
}

func TestProjectCacheDiscover_DiscoversAndValidates(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	parent := filepath.Join(home, "repos")
	repoDir := filepath.Join(parent, "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".todo", "a.md"), "a\n")
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	// ProjectPush auto-records the cache; clear it to test discovery.
	clearProjectCache(t, svc)

	result, err := ps.ProjectCacheDiscover(context.Background(), []string{parent})
	if err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}
	if len(result.Discovered) != 1 || result.Discovered[0] != "github.com/user/repo" {
		t.Errorf("discovered = %v, want [github.com/user/repo]", result.Discovered)
	}
	if len(result.Validated) != 0 {
		t.Errorf("validated = %v, want none", result.Validated)
	}

	// Re-discovering the same root should validate instead of discover.
	result, err = ps.ProjectCacheDiscover(context.Background(), []string{parent})
	if err != nil {
		t.Fatalf("ProjectCacheDiscover second pass: %v", err)
	}
	if len(result.Discovered) != 0 {
		t.Errorf("discovered = %v, want none on second pass", result.Discovered)
	}
	if len(result.Validated) != 1 || result.Validated[0] != "github.com/user/repo" {
		t.Errorf("validated = %v, want [github.com/user/repo]", result.Validated)
	}
}

func TestProjectCacheDiscover_MarksMissingWhenCheckoutGone(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	parent := filepath.Join(home, "repos")
	repoDir := filepath.Join(parent, "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".todo", "a.md"), "a\n")
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("push: %v", err)
	}

	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{parent}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}

	if err := os.RemoveAll(repoDir); err != nil {
		t.Fatal(err)
	}

	result, err := ps.ProjectCacheDiscover(context.Background(), []string{parent})
	if err != nil {
		t.Fatalf("ProjectCacheDiscover after removal: %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "github.com/user/repo" {
		t.Errorf("missing = %v, want [github.com/user/repo]", result.Missing)
	}
}

func TestCheckProjectCache(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	parent := filepath.Join(home, "repos")
	repoDir := filepath.Join(parent, "hermes")
	testhelpers.MakeDir(t, repoDir)
	initProjectRepo(t, repoDir)

	ps := service.NewProjectService(svc)
	if _, _, err := ps.ProjectAddPattern(context.Background(), repoDir, ".todo/**"); err != nil {
		t.Fatalf("add pattern: %v", err)
	}
	testhelpers.MakeFile(t, filepath.Join(repoDir, ".todo", "a.md"), "a\n")
	if _, err := ps.ProjectPush(context.Background(), repoDir, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	// ProjectPush auto-records the cache; clear it to test the uncached state.
	clearProjectCache(t, svc)

	// Without a cache entry the project is uncached.
	check, err := ps.CheckProjectCache(context.Background())
	if err != nil {
		t.Fatalf("CheckProjectCache: %v", err)
	}
	if len(check.Uncached) != 1 || check.Uncached[0] != "github.com/user/repo" {
		t.Errorf("uncached = %v, want [github.com/user/repo]", check.Uncached)
	}

	if _, err := ps.ProjectCacheDiscover(context.Background(), []string{parent}); err != nil {
		t.Fatalf("ProjectCacheDiscover: %v", err)
	}

	check, err = ps.CheckProjectCache(context.Background())
	if err != nil {
		t.Fatalf("CheckProjectCache after discover: %v", err)
	}
	if len(check.Available) != 1 || check.Available[0].ID != "github.com/user/repo" {
		t.Errorf("available = %v, want one github.com/user/repo entry", check.Available)
	}
}
