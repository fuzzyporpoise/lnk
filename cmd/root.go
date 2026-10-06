// Package cmd implements the v2 CLI.
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"go.fuzzyporpoise.dev/lnk/v2/service"
)

var (
	version   = "internal"
	buildTime = "unknown"
)

// SetVersion sets the build-time version and build timestamp injected by ldflags.
func SetVersion(v, bt string) {
	version = v
	buildTime = bt
}

// NewRootCommand constructs the v2 root command.
func NewRootCommand() *cobra.Command {
	var repoPath string

	rootCmd := &cobra.Command{
		Use:           "lnk",
		Short:         "lightweight git-native dotfiles management",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (built %s)", version, buildTime),
	}

	rootCmd.PersistentFlags().StringVar(&repoPath, "repo", "", "path to the lnk repository")
	rootCmd.PersistentFlags().Bool("no-project", false, "skip automatic project scope detection")

	rootCmd.AddCommand(newInitCmd(&repoPath))
	rootCmd.AddCommand(newCloneCmd(&repoPath))
	rootCmd.AddCommand(newAddCmd(&repoPath))
	rootCmd.AddCommand(newCreateCmd(&repoPath))
	rootCmd.AddCommand(newMoveCmd(&repoPath))
	rootCmd.AddCommand(newRemoveCmd(&repoPath))
	rootCmd.AddCommand(newForgetCmd(&repoPath))
	rootCmd.AddCommand(newListCmd(&repoPath))
	rootCmd.AddCommand(newStatusCmd(&repoPath))
	rootCmd.AddCommand(newDiffCmd(&repoPath))
	rootCmd.AddCommand(newCommitCmd(&repoPath))
	rootCmd.AddCommand(newPushCmd(&repoPath))
	rootCmd.AddCommand(newPullCmd(&repoPath))
	rootCmd.AddCommand(newRestoreCmd(&repoPath))
	rootCmd.AddCommand(newUpdateCmd(&repoPath))
	rootCmd.AddCommand(newDoctorCmd(&repoPath))
	rootCmd.AddCommand(newProjectCmd(&repoPath))
	rootCmd.AddCommand(newBootstrapCmd(&repoPath))
	rootCmd.AddCommand(newFormatCmd(&repoPath))
	rootCmd.AddCommand(newHomeCmd(&repoPath))
	rootCmd.AddCommand(newHooksCmd(&repoPath))

	return rootCmd
}

// Execute runs the v2 CLI.
func Execute() {
	rootCmd := NewRootCommand()
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// svc constructs a Service for the resolved repo path.
// Call once per command invocation and reuse the result.
func svc(repoFlag *string, opts ...service.Option) *service.Service {
	resolvedRepo := service.ResolveRepoPath(strings.TrimSpace(*repoFlag))
	return service.New(resolvedRepo, opts...)
}

// newInitCmd returns the "init" subcommand.
func newInitCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create or adopt a local lnk repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Init(cmd.Context()); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Initialized repo at %s\n", app.RepoPath())
			return err
		},
	}
}

// newHomeCmd returns the "home" subcommand.
func newHomeCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "home",
		Short: "Returns resolved lnk repo path",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s", app.RepoPath())
			return err
		},
	}
}

// newCloneCmd returns the "clone" subcommand.
func newCloneCmd(repoFlag *string) *cobra.Command {
	var withBootstrap bool

	cmd := &cobra.Command{
		Use:   "clone <url>",
		Short: "Clone a remote lnk repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			ran, err := app.Clone(cmd.Context(), args[0], withBootstrap, cmd.OutOrStdout(), cmd.ErrOrStderr(), os.Stdin)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Cloned repo to %s\n", app.RepoPath()); err != nil {
				return err
			}
			if !withBootstrap {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Run 'lnk restore' or 'lnk update' when ready")
				return err
			}
			if ran {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Ran bootstrap.sh")
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "No bootstrap.sh found")
			return err
		},
	}

	cmd.Flags().BoolVar(&withBootstrap, "bootstrap", false, "run bootstrap.sh after clone")
	return cmd
}

// newAddCmd returns the "add" subcommand.
func newAddCmd(repoFlag *string) *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "add [--host H] <path...>",
		Short: "Track one or more paths",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Add(cmd.Context(), host, args); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Tracked %d path(s) in %s scope\n", len(args), service.NormalizeHost(host))
			return err
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "track paths in a host-specific scope")
	return cmd
}

// newProjectCmd returns the "project" command group.
func newProjectCmd(repoFlag *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage project-local dotfiles",
	}
	cmd.PersistentFlags().String("dir", "", "project directory (default: current directory)")
	cmd.AddCommand(newProjectInitCmd(repoFlag))
	cmd.AddCommand(newProjectAddCmd(repoFlag))
	cmd.AddCommand(newProjectListCmd(repoFlag))
	cmd.AddCommand(newProjectUntrackCmd(repoFlag))
	cmd.AddCommand(newProjectPushCmd(repoFlag))
	cmd.AddCommand(newProjectSyncCmd(repoFlag))
	cmd.AddCommand(newProjectCacheCmd(repoFlag))
	cmd.AddCommand(newProjectRestoreCmd(repoFlag))
	cmd.AddCommand(newProjectPullCmd(repoFlag))
	cmd.AddCommand(newProjectRemoveCmd(repoFlag))
	cmd.AddCommand(newProjectForgetCmd(repoFlag))
	return cmd
}

// projectDir resolves the --dir flag (defaulting to the current working
// directory). The service layer anchors it at the enclosing git repo root.
func projectDir(cmd *cobra.Command) (string, error) {
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", dir, err)
	}

	return absDir, nil
}

// newProjectInitCmd returns the "project init" subcommand.
func newProjectInitCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Activate project scope for the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			ps := service.NewProjectService(svc(repoFlag))
			created, err := ps.ProjectInit(cmd.Context(), projectRoot)
			if err != nil {
				return err
			}

			if created {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Project scope initialized — global patterns apply. Add repo-local patterns with 'lnk project add <pattern>'.")
			} else {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Project scope already initialized.")
			}
			return err
		},
	}
}

// newProjectAddCmd returns the "project add" subcommand.
func newProjectAddCmd(repoFlag *string) *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "add [--global] <pattern...>",
		Short: "Add a pattern to .lnkinclude (local unless --global)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ps := service.NewProjectService(svc(repoFlag))

			if global {
				for _, pattern := range args {
					normalized, err := ps.ProjectAddGlobalPattern(pattern)
					if err != nil {
						return err
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Added '%s' to the global .lnkinclude — remember to commit it.\n", normalized); err != nil {
						return err
					}
				}
				return nil
			}

			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			for _, pattern := range args {
				normalized, matched, err := ps.ProjectAddPattern(cmd.Context(), projectRoot, pattern)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Added '%s' to .lnkinclude — remember to commit it.\n", normalized); err != nil {
					return err
				}
				if !matched {
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: '%s' matches no existing files; it will apply to future matches\n", normalized); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "add the pattern to the lnk repo's global .lnkinclude")
	return cmd
}

// newProjectListCmd returns the "project list" subcommand.
func newProjectListCmd(repoFlag *string) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "list [--all]",
		Short: "List effective patterns, or all stored projects with --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			ps := service.NewProjectService(svc(repoFlag))

			if all {
				projects, err := ps.ProjectListProjects()
				if err != nil {
					return err
				}
				if len(projects) == 0 {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), "No stored projects")
					return err
				}
				for _, p := range projects {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s (%d file(s))\n", p.ID, p.Files); err != nil {
						return err
					}
				}
				return nil
			}

			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			global, local, err := ps.ProjectListPatterns(cmd.Context(), projectRoot)
			if err != nil {
				return err
			}

			app := svc(repoFlag)
			globalPath := filepath.Join(app.RepoPath(), ".lnkinclude")
			localPath := filepath.Join(projectRoot, ".lnkinclude")

			if len(global) > 0 {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "# global (%s)\n", globalPath); err != nil {
					return err
				}
				for _, p := range global {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), p); err != nil {
						return err
					}
				}
			}
			if len(global) > 0 && len(local) > 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			if len(local) > 0 {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "# local (%s)\n", localPath); err != nil {
					return err
				}
				for _, p := range local {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), p); err != nil {
						return err
					}
				}
			}
			if len(global) == 0 && len(local) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "# no patterns defined")
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "list stored projects instead of patterns")
	return cmd
}

// newProjectUntrackCmd returns the "project untrack" subcommand.
func newProjectUntrackCmd(repoFlag *string) *cobra.Command {
	var keep bool
	var global bool

	cmd := &cobra.Command{
		Use:   "untrack [--keep] [--global] <pattern>",
		Short: "Remove a pattern from .lnkinclude and unmanage its files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ps := service.NewProjectService(svc(repoFlag))

			if global {
				if _, err := ps.ProjectUntrackGlobalPattern(args[0]); err != nil {
					return err
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed '%s' from the global .lnkinclude — remember to commit it.\n", args[0])
				return err
			}

			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			result, err := ps.ProjectUntrackPattern(cmd.Context(), projectRoot, args[0], keep)
			if err != nil {
				return err
			}

			if result.IsGlobal {
				app := svc(repoFlag)
				globalPath := filepath.Join(app.RepoPath(), ".lnkinclude")
				_, err = fmt.Fprintf(cmd.ErrOrStderr(), "This pattern comes from the global .lnkinclude — edit %s to remove it, or use --global.\n", globalPath)
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed '%s' from .lnkinclude — remember to commit it.\n", args[0]); err != nil {
				return err
			}
			if len(result.Released) > 0 {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Restored %d file(s) to the project:\n", len(result.Released)); err != nil {
					return err
				}
				for _, path := range result.Released {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", path); err != nil {
						return err
					}
				}
			}
			for _, path := range result.BackedUp {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: existing file backed up to %s.lnk-backup\n", path); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&keep, "keep", false, "only edit .lnkinclude, leaving managed files in place")
	cmd.Flags().BoolVar(&global, "global", false, "remove the pattern from the lnk repo's global .lnkinclude")
	return cmd
}

// newProjectSyncCmd returns the "project sync" subcommand.
func newProjectSyncCmd(repoFlag *string) *cobra.Command {
	var dryRun bool
	var pruneDeletions bool
	var force bool
	var all bool

	cmd := &cobra.Command{
		Use:   "sync [--all] [--dry-run] [--prune-deletions] [--force]",
		Short: "Reconcile patterns, live files, and project storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			ps := service.NewProjectService(svc(repoFlag))

			if all {
				result, err := ps.ProjectSyncAll(cmd.Context(), dryRun, pruneDeletions, force)
				if err != nil {
					return err
				}
				return printProjectSyncAll(cmd.OutOrStdout(), cmd.ErrOrStderr(), result, dryRun)
			}

			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			result, err := ps.ProjectSync(cmd.Context(), projectRoot, dryRun, pruneDeletions, force)
			if err != nil {
				return err
			}

			return printProjectSync(cmd.OutOrStdout(), cmd.ErrOrStderr(), result, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview reconciliation without changing files")
	cmd.Flags().BoolVar(&pruneDeletions, "prune-deletions", false, "delete stored files whose live copies were deleted")
	cmd.Flags().BoolVar(&force, "force", false, "also manage files tracked by the project's own git")
	cmd.Flags().BoolVar(&all, "all", false, "reconcile every stored project using the local project registry")
	return cmd
}

// newProjectCacheCmd returns the "project cache" subcommand.
func newProjectCacheCmd(repoFlag *string) *cobra.Command {
	var scanRoots []string

	cmd := &cobra.Command{
		Use:   "cache --scan path [--scan path]...",
		Short: "Discover local project checkouts and update the local project registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			ps := service.NewProjectService(svc(repoFlag))
			result, err := ps.ProjectCacheDiscover(cmd.Context(), scanRoots)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if len(result.Discovered) > 0 {
				if _, err := fmt.Fprintf(w, "Discovered %d project(s):\n", len(result.Discovered)); err != nil {
					return err
				}
				for _, id := range result.Discovered {
					if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
						return err
					}
				}
			}
			if len(result.Validated) > 0 {
				if _, err := fmt.Fprintf(w, "Validated %d project(s):\n", len(result.Validated)); err != nil {
					return err
				}
				for _, id := range result.Validated {
					if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
						return err
					}
				}
			}
			if len(result.Missing) > 0 {
				if _, err := fmt.Fprintf(w, "Marked %d project(s) as missing:\n", len(result.Missing)); err != nil {
					return err
				}
				for _, id := range result.Missing {
					if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
						return err
					}
				}
			}
			if len(result.Removed) > 0 {
				if _, err := fmt.Fprintf(w, "Removed %d stale cache entry(ies):\n", len(result.Removed)); err != nil {
					return err
				}
				for _, id := range result.Removed {
					if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
						return err
					}
				}
			}
			if len(result.Discovered)+len(result.Validated)+len(result.Missing)+len(result.Removed) == 0 {
				_, err = fmt.Fprintln(w, "No changes to project cache")
			}
			return err
		},
	}

	cmd.Flags().StringArrayVar(&scanRoots, "scan", nil, "directory to scan for local project checkouts (repeatable)")
	_ = cmd.MarkFlagRequired("scan")
	return cmd
}

// printProjectSync writes the output for a single project sync result.
func printProjectSync(w, errW io.Writer, result service.ProjectSyncResult, dryRun bool) error {
	if err := printSyncSection(w, dryRunPrefix(dryRun, "Synced", "Would sync"), result.Synced, "file(s) to project storage"); err != nil {
		return err
	}
	if err := printSyncSection(w, dryRunPrefix(dryRun, "Restored", "Would restore"), result.Released, "file(s) to the project"); err != nil {
		return err
	}
	if err := printSyncSection(w, dryRunPrefix(dryRun, "Backed up", "Would back up"), result.BackedUp, "conflicting file(s)"); err != nil {
		return err
	}
	if err := printSyncSection(w, dryRunPrefix(dryRun, "Pruned", "Would prune"), result.Pruned, "stored file(s) deleted from the project"); err != nil {
		return err
	}
	if len(result.Deletions) > 0 {
		if _, err := fmt.Fprintf(w, "%d stored file(s) no longer exist in the project (run with --prune-deletions to drop them):\n", len(result.Deletions)); err != nil {
			return err
		}
		for _, path := range result.Deletions {
			if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
				return err
			}
		}
	}
	for _, path := range result.SkippedTracked {
		if _, err := fmt.Fprintf(errW, "warning: skipped '%s': tracked by this repo's git (add '!%s' to .lnkinclude, or use --force)\n", path, path); err != nil {
			return err
		}
	}

	if len(result.Synced)+len(result.Released)+len(result.Pruned)+len(result.Deletions) == 0 {
		_, err := fmt.Fprintln(w, "Project storage is in sync with the effective patterns")
		return err
	}
	return nil
}

// printProjectSyncAll writes the output for `lnk project sync --all`.
func printProjectSyncAll(w, errW io.Writer, result service.ProjectSyncAllResult, dryRun bool) error {
	for _, res := range result.Results {
		if _, err := fmt.Fprintf(w, "# %s\n", res.ProjectID); err != nil {
			return err
		}
		if err := printProjectSync(w, errW, res, dryRun); err != nil {
			return err
		}
	}
	for _, id := range result.Unavailable {
		if _, err := fmt.Fprintf(errW, "warning: skipping %s: local checkout not found in scan roots\n", id); err != nil {
			return err
		}
	}
	if len(result.Results) == 0 && len(result.Unavailable) == 0 {
		_, err := fmt.Fprintln(w, "No stored projects")
		return err
	}
	return nil
}

// printSyncSection writes one titled list section of sync output.
func printSyncSection(w io.Writer, title string, paths []string, suffix string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s %d %s:\n", title, len(paths), suffix); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
			return err
		}
	}
	return nil
}

// newProjectRemoveCmd returns the "project remove" subcommand.
func newProjectRemoveCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Stop managing this project: restore its files and delete storage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			ps := service.NewProjectService(svc(repoFlag))
			result, err := ps.ProjectRemove(cmd.Context(), projectRoot)
			if err != nil {
				return err
			}

			if err := printRestore(cmd.OutOrStdout(), service.RestoreInfo{Restored: result.Restored, BackedUp: result.BackedUp}, false); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed project storage for %s\n", result.ProjectID); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), ".lnkinclude was left in place; delete it to give up the patterns")
			return err
		},
	}
}

// newProjectForgetCmd returns the "project forget" subcommand.
func newProjectForgetCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "forget",
		Short: "Stop managing this project but keep its stored files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			ps := service.NewProjectService(svc(repoFlag))
			result, err := ps.ProjectForget(cmd.Context(), projectRoot)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if len(result.Unlinked) == 0 {
				_, err = fmt.Fprintln(w, "No managed symlinks found")
			} else {
				if _, err := fmt.Fprintf(w, "Removed %d symlink(s) from the project:\n", len(result.Unlinked)); err != nil {
					return err
				}
				for _, path := range result.Unlinked {
					if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
						return err
					}
				}
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(w, "Stored files kept; run 'lnk project restore' to bring them back")
			return err
		},
	}
}
func newProjectPushCmd(repoFlag *string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "push [--force]",
		Short: "Push matching project files into lnk storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			ps := service.NewProjectService(svc(repoFlag))
			result, err := ps.ProjectPush(cmd.Context(), projectRoot, force)
			if err != nil {
				return err
			}

			for _, path := range result.SkippedTracked {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipped '%s': tracked by this repo's git (add '!%s' to .lnkinclude, or use --force)\n", path, path); err != nil {
					return err
				}
			}

			if len(result.Synced) == 0 {
				if len(result.SkippedTracked) == 0 {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), "Nothing to sync — all tracked files are already up to date")
				}
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Synced %d file(s) to project storage\n", len(result.Synced)); err != nil {
				return err
			}
			for _, path := range result.Synced {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", path); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "also manage files tracked by the project's own git")
	return cmd
}

// newProjectRestoreCmd returns the "project restore" subcommand.
func newProjectRestoreCmd(repoFlag *string) *cobra.Command {
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:   "restore [--dry-run] [--force]",
		Short: "Recreate symlinks for project files from storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			ps := service.NewProjectService(svc(repoFlag))
			info, err := ps.ProjectRestore(cmd.Context(), projectRoot, dryRun, force)
			if err != nil {
				return err
			}
			return printRestore(cmd.OutOrStdout(), info, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview restore actions without changing files")
	cmd.Flags().BoolVar(&force, "force", false, "replace files tracked by the project's own git")
	return cmd
}

// newProjectPullCmd returns the "project pull" subcommand.
func newProjectPullCmd(repoFlag *string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "pull [--force]",
		Short: "Pull lnk repo changes and restore project symlinks",
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := projectDir(cmd)
			if err != nil {
				return err
			}

			ps := service.NewProjectService(svc(repoFlag))
			info, err := ps.ProjectPull(cmd.Context(), projectRoot, force)
			if err != nil {
				return err
			}
			if err := printRestore(cmd.OutOrStdout(), info, false); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Pulled project changes")
			return err
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "replace files tracked by the project's own git")
	return cmd
}

// newMoveCmd returns the "move" subcommand.
func newMoveCmd(repoFlag *string) *cobra.Command {
	var toCommon bool
	var toHost string

	cmd := &cobra.Command{
		Use:   "move <path> (--to-common | --to-host H)",
		Short: "Move a tracked path between scopes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Move(cmd.Context(), args[0], toHost, toCommon); err != nil {
				return err
			}
			target := service.NormalizeHost(toHost)
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Moved %s to %s scope\n", args[0], target)
			return err
		},
	}

	cmd.Flags().BoolVar(&toCommon, "to-common", false, "move the path into common scope")
	cmd.Flags().StringVar(&toHost, "to-host", "", "move the path into a host-specific scope")
	cmd.MarkFlagsMutuallyExclusive("to-common", "to-host")
	return cmd
}

// newRemoveCmd returns the "remove" subcommand.
func newRemoveCmd(repoFlag *string) *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "remove [--host H] <path>",
		Short: "Stop managing a path and restore it locally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Remove(cmd.Context(), host, args[0]); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s scope\n", args[0], service.NormalizeHost(host))
			return err
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "remove a host-scoped path")
	return cmd
}

// newForgetCmd returns the "forget" subcommand.
func newForgetCmd(repoFlag *string) *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "forget [--host H] <path>",
		Short: "Stop managing a path but keep its stored repo copy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Forget(cmd.Context(), host, args[0]); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Forgot %s from %s scope\n", args[0], service.NormalizeHost(host))
			return err
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "forget a host-scoped path")
	return cmd
}

// newListCmd returns the "list" subcommand.
func newListCmd(repoFlag *string) *cobra.Command {
	var host string
	var all bool

	cmd := &cobra.Command{
		Use:   "list [--host H | --all]",
		Short: "List tracked paths by storage scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			result, err := app.List(cmd.Context(), host, all)
			if err != nil {
				return err
			}
			for i, scope := range result.Scopes {
				if i > 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return err
					}
				}
				header := scope.Name
				if scope.Name != "common" {
					if scope.Active {
						header += " [active]"
					} else {
						header += " [not installed]"
					}
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", header); err != nil {
					return err
				}
				if len(scope.Items) == 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "  (no files)"); err != nil {
						return err
					}
					continue
				}
				for _, item := range scope.Items {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", item); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "list one host scope")
	cmd.Flags().BoolVar(&all, "all", false, "list common plus all host scopes")
	cmd.MarkFlagsMutuallyExclusive("all", "host")
	return cmd
}

// newStatusCmd returns the "status" subcommand.
func newStatusCmd(repoFlag *string) *cobra.Command {
	var color bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show repo sync status",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag, service.WithColor(color))

			status, err := app.Status(cmd.Context())
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			if status.Remote == "" {
				if _, err := fmt.Fprintln(writer, "Remote not set"); err != nil {
					return err
				}
			}
			// Print the full git status output
			if _, err := io.WriteString(writer, status.Output); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&color, "color", false, "set to display `git status` in color, default is no color")
	return cmd
}

// newDiffCmd returns the "diff" subcommand.
func newDiffCmd(repoFlag *string) *cobra.Command {
	var color bool

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show the uncommitted repo diff",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag, service.WithColor(color))

			diff, err := app.Diff(cmd.Context())
			if err != nil {
				return err
			}
			if diff == "" {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No uncommitted changes")
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), diff)
			return err
		},
	}

	cmd.Flags().BoolVar(&color, "color", false, "set to display `git diff` in color, default is no color")
	return cmd
}

// newCommitCmd returns the "commit" subcommand.
func newCommitCmd(repoFlag *string) *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Stage all repo changes and create a commit",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if message == "" {
				message = "lnk: sync configuration files"
			}
			if err := app.Commit(cmd.Context(), message); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Committed repo changes: %s\n", message)
			return err
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	return cmd
}

// newPushCmd returns the "push" subcommand.
func newPushCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Push existing commits only",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Push(cmd.Context()); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Pushed existing commits")
			return err
		},
	}
}

// newPullCmd returns the "pull" subcommand.
func newPullCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull repo changes only",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			if err := app.Pull(cmd.Context()); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Pulled repo changes")
			return err
		},
	}
}

// newRestoreCmd returns the "restore" subcommand.
func newRestoreCmd(repoFlag *string) *cobra.Command {
	var host string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "restore [--host H] [--dry-run]",
		Short: "Restore the effective machine profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			noProject, _ := cmd.Flags().GetBool("no-project")
			info, err := app.RestoreWithProject(cmd.Context(), host, noProject, dryRun)
			if err != nil {
				return err
			}
			return printRestore(cmd.OutOrStdout(), info, dryRun)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "include one host scope in the restored profile")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview restore actions without changing files")
	return cmd
}

// newUpdateCmd returns the "update" subcommand.
func newUpdateCmd(repoFlag *string) *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "update [--host H]",
		Short: "Pull repo changes and restore the effective machine profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			noProject, _ := cmd.Flags().GetBool("no-project")
			info, err := app.UpdateWithProject(cmd.Context(), host, noProject)
			if err != nil {
				return err
			}
			if err := printRestore(cmd.OutOrStdout(), info, false); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Updated repo and machine state")
			return err
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "include one host scope in the restored profile")
	return cmd
}

// newDoctorCmd returns the "doctor" subcommand.
func newDoctorCmd(repoFlag *string) *cobra.Command {
	var host string
	var all bool
	var fix bool
	var pruneEmpty bool

	cmd := &cobra.Command{
		Use:   "doctor [--host H | --all] [--fix] [--prune-empty]",
		Short: "Audit repo and profile health",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			report, err := app.Doctor(cmd.Context(), host, all, fix, pruneEmpty)
			if err != nil {
				return err
			}
			return printDoctor(cmd.OutOrStdout(), report)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "check one host profile")
	cmd.Flags().BoolVar(&all, "all", false, "check all storage scopes")
	cmd.Flags().BoolVar(&fix, "fix", false, "apply safe automatic fixes")
	cmd.Flags().BoolVar(&pruneEmpty, "prune-empty", false, "remove empty host scopes and project storage when passed with --fix")
	cmd.MarkFlagsMutuallyExclusive("all", "host")
	return cmd
}

// newFormatCmd returns the "format" subcommand.
func newFormatCmd(repoFlag *string) *cobra.Command {
	var ver1 bool
	var ver2 bool

	cmd := &cobra.Command{
		Use:   "format [--v1 | --v2]",
		Short: "Update format of common lnks",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			result, err := app.Format(cmd.Context(), ver1, ver2)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), result)
			return err
		},
	}

	cmd.Flags().BoolVar(&ver1, "v1", false, "legacy format, with dotfiles and folders in root directory")
	cmd.Flags().BoolVar(&ver2, "v2", false, "version2 format, common dotfiles aggregated under common.lnk")
	cmd.MarkFlagsMutuallyExclusive("v1", "v2")
	return cmd
}

// newBootstrapCmd returns the "bootstrap" subcommand.
func newBootstrapCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Run bootstrap.sh explicitly",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			ran, err := app.Bootstrap(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), os.Stdin)
			if err != nil {
				return err
			}
			if !ran {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No bootstrap.sh found")
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Ran bootstrap.sh")
			return err
		},
	}
}

// dryRunPrefix selects a prefix string based on whether dry-run mode is active.
func dryRunPrefix(dryRun bool, live, dry string) string {
	if dryRun {
		return dry
	}
	return live
}

// printRestore writes restore/update results to w.
func printRestore(w io.Writer, info service.RestoreInfo, dryRun bool) error {
	prefix := dryRunPrefix(dryRun, "Restored", "Would restore")
	backupPrefix := dryRunPrefix(dryRun, "Backed up", "Would back up")

	if _, err := fmt.Fprintf(w, "%s %d path(s)\n", prefix, len(info.Restored)); err != nil {
		return err
	}
	for _, path := range info.Restored {
		if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
			return err
		}
	}
	if len(info.BackedUp) > 0 {
		if _, err := fmt.Fprintf(w, "%s %d conflicting path(s)\n", backupPrefix, len(info.BackedUp)); err != nil {
			return err
		}
		for _, path := range info.BackedUp {
			if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
				return err
			}
		}
	}
	if len(info.Collisions) > 0 {
		if _, err := fmt.Fprintf(w, "Skipped %d path(s) with existing files (collisions reported by hook)\n", len(info.Collisions)); err != nil {
			return err
		}
		for _, path := range info.Collisions {
			if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
				return err
			}
		}
	}
	if len(info.SkippedTracked) == 0 && len(info.SkippedUnmatched) == 0 {
		return nil
	}
	if len(info.SkippedTracked) > 0 {
		if _, err := fmt.Fprintf(w, "Skipped %d path(s) tracked by the project's git (use --force to manage them)\n", len(info.SkippedTracked)); err != nil {
			return err
		}
		for _, path := range info.SkippedTracked {
			if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
				return err
			}
		}
	}
	if len(info.SkippedUnmatched) > 0 {
		if _, err := fmt.Fprintf(w, "Skipped %d stored path(s) that no longer match patterns (run 'lnk project sync' to reconcile)\n", len(info.SkippedUnmatched)); err != nil {
			return err
		}
		for _, path := range info.SkippedUnmatched {
			if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
				return err
			}
		}
	}
	return nil
}

// printDoctor writes the doctor report to w.
func printDoctor(w io.Writer, report service.DoctorReport) error {
	if _, err := fmt.Fprintf(w, "Mode: %s\n", report.Mode); err != nil {
		return err
	}
	if report.MarkerMissing {
		line := "Repo marker missing"
		if report.MarkerFixed {
			line = "Repo marker was added"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if report.LegacyCommitIdentity {
		line := "Legacy lnk commit identity in .git/config shadows your own git identity"
		if report.LegacyCommitIdentityFixed {
			line = "Removed legacy lnk commit identity from .git/config"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if len(report.Collisions) > 0 {
		if _, err := fmt.Fprintln(w, "Ownership collisions:"); err != nil {
			return err
		}
		for _, collision := range report.Collisions {
			if _, err := fmt.Fprintf(w, "  %s -> %s\n", collision.Path, strings.Join(collision.Scopes, ", ")); err != nil {
				return err
			}
		}
	}
	for _, result := range report.ScopeResults {
		if err := result.Print(w); err != nil {
			return err
		}
	}
	if len(report.Projects) > 0 {
		if _, err := fmt.Fprintln(w, "Projects:"); err != nil {
			return err
		}
		for _, p := range report.Projects {
			if _, err := fmt.Fprintf(w, "  %s (%d file(s))\n", p.ID, p.Files); err != nil {
				return err
			}
		}
	}
	if len(report.UnmarkedProjects) > 0 {
		if _, err := fmt.Fprintln(w, "Stored projects without a marker:"); err != nil {
			return err
		}
		for _, p := range report.UnmarkedProjects {
			if _, err := fmt.Fprintf(w, "  %s\n", p); err != nil {
				return err
			}
		}
	}
	if len(report.EmptyProjects) > 0 {
		if _, err := fmt.Fprintln(w, "Empty project storage:"); err != nil {
			return err
		}
		for _, p := range report.EmptyProjects {
			if _, err := fmt.Fprintf(w, "  %s\n", p); err != nil {
				return err
			}
		}
	}
	if len(report.ProjectIssues) > 0 {
		if _, err := fmt.Fprintln(w, "Project issues:"); err != nil {
			return err
		}
		for _, issue := range report.ProjectIssues {
			if _, err := fmt.Fprintf(w, "  [%s] %s: %s", issue.Severity, issue.ProjectID, issue.Issue); err != nil {
				return err
			}
			if issue.Suggestion != "" {
				if _, err := fmt.Fprintf(w, " -> %s", issue.Suggestion); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
	}
	if len(report.PrunedProjects) > 0 {
		if _, err := fmt.Fprintln(w, "Pruned empty project storage:"); err != nil {
			return err
		}
		for _, p := range report.PrunedProjects {
			if _, err := fmt.Fprintf(w, "  %s\n", p); err != nil {
				return err
			}
		}
	}
	if len(report.EmptyScopes) > 0 {
		if _, err := fmt.Fprintln(w, "\nEmpty host scopes:"); err != nil {
			return err
		}
		for _, scope := range report.EmptyScopes {
			if _, err := fmt.Fprintf(w, "  %s\n", scope); err != nil {
				return err
			}
		}
	}
	if len(report.PrunedScopes) > 0 {
		if _, err := fmt.Fprintln(w, "Pruned empty host scopes:"); err != nil {
			return err
		}
		for _, scope := range report.PrunedScopes {
			if _, err := fmt.Fprintf(w, "  %s\n", scope); err != nil {
				return err
			}
		}
	}
	if report.BrokenSymlinkFixSkipped {
		_, err := fmt.Fprintln(w, "Broken symlink repair was skipped in --all mode")
		return err
	}
	if report.BrokenSymlinkFix {
		_, err := fmt.Fprintln(w, "Broken symlinks repaired")
		return err
	}
	return nil
}
