package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"go.fuzzyporpoise.dev/lnk/v2/internal/gitboundary"
	"go.fuzzyporpoise.dev/lnk/v2/internal/hooks"
)

// newHooksCmd returns the "hooks" command group.
func newHooksCmd(repoFlag *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Install and run git hooks for lnk",
	}
	cmd.AddCommand(newHooksInstallCmd(repoFlag))
	cmd.AddCommand(newHooksUninstallCmd(repoFlag))
	cmd.AddCommand(newHooksRunCmd(repoFlag))
	return cmd
}

// newHooksInstallCmd returns the "hooks install" subcommand.
func newHooksInstallCmd(repoFlag *string) *cobra.Command {
	var project bool

	cmd := &cobra.Command{
		Use:   "install [--project]",
		Short: "Install lnk git hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			lnkBinary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve lnk executable: %w", err)
			}

			if project {
				projectRoot, err := resolveProjectRoot(cmd.Context())
				if err != nil {
					return err
				}
				if err := hooks.InstallProject(projectRoot, lnkBinary); err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Installed post-checkout hook in project")
				return err
			}

			app := svc(repoFlag)
			if err := hooks.InstallLnkRepo(app.RepoPath(), lnkBinary); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Installed post-merge hook in lnk repo")
			return err
		},
	}

	cmd.Flags().BoolVar(&project, "project", false, "install the post-checkout hook in the current project repo")
	return cmd
}

// newHooksUninstallCmd returns the "hooks uninstall" subcommand.
func newHooksUninstallCmd(repoFlag *string) *cobra.Command {
	var project bool

	cmd := &cobra.Command{
		Use:   "uninstall [--project]",
		Short: "Uninstall lnk git hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project {
				projectRoot, err := resolveProjectRoot(cmd.Context())
				if err != nil {
					return err
				}
				if err := hooks.UninstallProject(projectRoot); err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Uninstalled post-checkout hook from project")
				return err
			}

			app := svc(repoFlag)
			if err := hooks.UninstallLnkRepo(app.RepoPath()); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Uninstalled post-merge hook from lnk repo")
			return err
		},
	}

	cmd.Flags().BoolVar(&project, "project", false, "uninstall the post-checkout hook from the current project repo")
	return cmd
}

// newHooksRunCmd returns the "hooks run" subcommand.
func newHooksRunCmd(repoFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run <hook-name> [args...]",
		Short: "Run a lnk hook (used by installed git hook scripts)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)
			return app.RunHook(cmd.Context(), args[0], args[1:], cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

// resolveProjectRoot returns the root of the project repository containing
// the current working directory.
func resolveProjectRoot(ctx context.Context) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	root, err := gitboundary.ResolveGitRoot(ctx, cwd)
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", fmt.Errorf("not inside a git repository")
	}

	return root, nil
}
