package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.fuzzyporpoise.dev/lnk/v2/internal/testhelpers"
)

// ---------- Move tests ----------

func TestMove_CommonToHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "testhost", false); err != nil {
		t.Fatalf("Move common→host: %v", err)
	}

	// Should now be tracked in host scope, not common.
	testhelpers.AssertTrackedInScope(t, repoPath, "testhost", ".bashrc")
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")

	// File should have moved to host storage.
	hostStorage := filepath.Join(repoPath, "testhost.lnk", ".bashrc")
	if !testhelpers.FileExists(t, hostStorage) {
		t.Errorf("expected file at host storage %q", hostStorage)
	}
	commonStorage := filepath.Join(repoPath, "common.lnk", ".bashrc")
	if testhelpers.FileExists(t, commonStorage) {
		t.Errorf("expected file removed from common storage %q", commonStorage)
	}
}

func TestMove_HostToCommon_BoolFlag(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "", true); err != nil {
		t.Fatalf("Move host→common (bool): %v", err)
	}

	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")

	commonStorage := filepath.Join(repoPath, "common.lnk", ".bashrc")
	if !testhelpers.FileExists(t, commonStorage) {
		t.Errorf("expected file at common storage %q", commonStorage)
	}
	hostStorage := filepath.Join(repoPath, "testhost.lnk", ".bashrc")
	if testhelpers.FileExists(t, hostStorage) {
		t.Errorf("expected file removed from host storage %q", hostStorage)
	}
}

func TestMove_HostToCommon_ExplicitCommonString(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")

	// toHost="common" with toCommon=false should succeed and route to common scope.
	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "common", false); err != nil {
		t.Fatalf("Move host→common (explicit string): %v", err)
	}

	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")

	commonStorage := filepath.Join(repoPath, "common.lnk", ".bashrc")
	if !testhelpers.FileExists(t, commonStorage) {
		t.Errorf("expected file at common storage %q", commonStorage)
	}
}

// ---------- Symlink repoint tests ----------

func TestMove_SymlinkRepointed_CommonToHost(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	livePath := filepath.Join(home, ".bashrc")

	if err := svc.Move(context.Background(), livePath, "testhost", false); err != nil {
		t.Fatalf("Move: %v", err)
	}

	// Live symlink should now point to host storage, not common storage.
	newStorage := filepath.Join(repoPath, "testhost.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, livePath, newStorage)
}

func TestMove_SymlinkRepointed_HostToCommon(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")
	livePath := filepath.Join(home, ".bashrc")

	if err := svc.Move(context.Background(), livePath, "", true); err != nil {
		t.Fatalf("Move: %v", err)
	}

	newStorage := filepath.Join(repoPath, "common.lnk", ".bashrc")
	testhelpers.AssertSymlink(t, livePath, newStorage)
}

// ---------- Failure cases ----------

func TestMove_NeitherFlagSet(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "", false)
	if err == nil {
		t.Fatal("expected error when neither --to-common nor --to-host is set, got nil")
	}
}

func TestMove_BothFlagsSet(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "testhost", true)
	if err == nil {
		t.Fatal("expected error when both --to-common and --to-host are set, got nil")
	}
}

func TestMove_PathNotManaged(t *testing.T) {
	svc, home := testhelpers.TestHome(t)

	filePath := filepath.Join(home, ".bashrc")
	testhelpers.MakeFile(t, filePath, "# bashrc")

	err := svc.Move(context.Background(), filePath, "testhost", false)
	if err == nil {
		t.Fatal("expected error moving unmanaged path, got nil")
	}
}

func TestMove_AlreadyInTargetScope(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	// Trying to move to common when it's already in common.
	err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "", true)
	if err == nil {
		t.Fatal("expected error moving path to scope it already belongs to, got nil")
	}
}

func TestMove_TargetScopeAlreadyOwnsPath(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	// Set up the same relative path in both scopes directly on disk.
	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# common bashrc")
	// Manually write the entry into the host tracker to simulate a collision
	// without going through Add.
	hostTrackerPath := filepath.Join(repoPath, ".lnk.testhost")
	if err := os.WriteFile(hostTrackerPath, []byte(".bashrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "testhost", false)
	if err == nil {
		t.Fatal("expected error when target scope already owns path, got nil")
	}
}

func TestMove_CommitCreated(t *testing.T) {
	svc, home := testhelpers.TestHome(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")
	commitsBefore := testhelpers.GitLog(t, repoPath)

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "testhost", false); err != nil {
		t.Fatalf("Move: %v", err)
	}

	commitsAfter := testhelpers.GitLog(t, repoPath)
	if len(commitsAfter) != len(commitsBefore)+1 {
		t.Errorf("expected 1 new commit after move, before=%d after=%d", len(commitsBefore), len(commitsAfter))
	}
}

// ---------- V1 Move tests ----------

func TestMove_V1_CommonToHost(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "testhost", false); err != nil {
		t.Fatalf("Move v1 common→host: %v", err)
	}

	testhelpers.AssertTrackedInScope(t, repoPath, "testhost", ".bashrc")
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")

	// v1 stores common files in repo root.
	if testhelpers.FileExists(t, filepath.Join(repoPath, ".bashrc")) {
		t.Error("expected file removed from v1 common storage (repo root)")
	}
	if !testhelpers.FileExists(t, filepath.Join(repoPath, "testhost.lnk", ".bashrc")) {
		t.Error("expected file at host storage")
	}
}

func TestMove_V1_HostToCommon(t *testing.T) {
	svc, home := testhelpers.TestHomeV1(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "", true); err != nil {
		t.Fatalf("Move v1 host→common: %v", err)
	}

	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")

	// v1 common storage is repo root.
	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".bashrc")) {
		t.Error("expected file at v1 common storage (repo root)")
	}
	if testhelpers.FileExists(t, filepath.Join(repoPath, "testhost.lnk", ".bashrc")) {
		t.Error("expected file removed from host storage")
	}
}

// ---------- V1 legacy Move tests ----------

func TestMove_V1Legacy_CommonToHost(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "common", ".bashrc", "# bashrc")

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "testhost", false); err != nil {
		t.Fatalf("Move v1 legacy common→host: %v", err)
	}

	testhelpers.AssertTrackedInScope(t, repoPath, "testhost", ".bashrc")
	testhelpers.AssertNotTracked(t, repoPath, ".bashrc")

	if testhelpers.FileExists(t, filepath.Join(repoPath, ".bashrc")) {
		t.Error("expected file removed from v1 legacy common storage (repo root)")
	}
	if !testhelpers.FileExists(t, filepath.Join(repoPath, "testhost.lnk", ".bashrc")) {
		t.Error("expected file at host storage")
	}
}

func TestMove_V1Legacy_HostToCommon(t *testing.T) {
	svc, home := testhelpers.TestHomeV1Legacy(t)
	repoPath := svc.RepoPath()

	setupTrackedFile(t, repoPath, home, "testhost", ".bashrc", "# bashrc")

	if err := svc.Move(context.Background(), filepath.Join(home, ".bashrc"), "", true); err != nil {
		t.Fatalf("Move v1 legacy host→common: %v", err)
	}

	testhelpers.AssertTracked(t, repoPath, ".bashrc")
	testhelpers.AssertNotTrackedInScope(t, repoPath, "testhost", ".bashrc")

	if !testhelpers.FileExists(t, filepath.Join(repoPath, ".bashrc")) {
		t.Error("expected file at v1 legacy common storage (repo root)")
	}
	if testhelpers.FileExists(t, filepath.Join(repoPath, "testhost.lnk", ".bashrc")) {
		t.Error("expected file removed from host storage")
	}
}
