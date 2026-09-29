package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
)

// hookTimeout limits how long a hook body may block the git operation.
const hookTimeout = 30 * time.Second

// RunHook executes the named hook on behalf of a git hook script. It never
// returns a non-nil error; failures are written to errOut as warnings so the
// underlying git operation is never blocked.
func (s *Service) RunHook(ctx context.Context, hookName string, args []string, out, errOut io.Writer) error {
	switch hookName {
	case "post-merge":
		return runWithTimeout(ctx, out, errOut, s.runPostMerge)
	case "post-checkout":
		return runWithTimeout(ctx, out, errOut, s.runPostCheckout)
	default:
		_, _ = fmt.Fprintf(errOut, "warning: unknown lnk hook %q\n", hookName)
		return nil
	}
}

// runWithTimeout runs fn with a bounded timeout and swallows errors after
// printing them to errOut. This ensures a hung or failing hook never blocks
// the git operation that invoked it.
func runWithTimeout(ctx context.Context, out, errOut io.Writer, fn func(context.Context, io.Writer) error) error {
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	if err := fn(ctx, out); err != nil {
		_, _ = fmt.Fprintf(errOut, "warning: lnk hook: %s\n", err.Error())
	}
	return nil
}

func (s *Service) runPostMerge(ctx context.Context, out io.Writer) error {
	info, err := s.RestoreHook(ctx)
	if err != nil {
		return err
	}
	if len(info.Restored) == 0 && len(info.Collisions) == 0 {
		return nil
	}
	return printRestoreHook(out, info)
}

func (s *Service) runPostCheckout(ctx context.Context, out io.Writer) error {
	// Git runs hooks with the repo root as the working directory. Resolve it
	// explicitly so the hook works even if cwd is a subdirectory.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	root, err := gitboundary.ResolveGitRoot(ctx, cwd)
	if err != nil {
		return err
	}
	if root == "" {
		return nil
	}

	// Do not treat the lnk repository as a project repository.
	if s.isLnkRepoRoot(root) {
		return nil
	}

	ps := NewProjectService(s)
	info, err := ps.ProjectRestoreHook(ctx, root)
	if err != nil {
		return err
	}
	if len(info.Restored) == 0 && len(info.Collisions) == 0 {
		return nil
	}
	return printRestoreHook(out, info)
}

// printRestoreHook writes hook restore results to w.
func printRestoreHook(w io.Writer, info RestoreInfo) error {
	for _, path := range info.Restored {
		if _, err := fmt.Fprintf(w, "lnk hook: restored %s\n", path); err != nil {
			return err
		}
	}
	for _, path := range info.Collisions {
		if _, err := fmt.Fprintf(w, "lnk hook: collision at %s (left untouched)\n", path); err != nil {
			return err
		}
	}
	return nil
}
