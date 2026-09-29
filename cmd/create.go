package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"go.fuzzyporpoise.dev/lnk/v2/internal/lnkerror"
	"go.fuzzyporpoise.dev/lnk/v2/service"
)

// newCreateCmd returns the "create" subcommand.
func newCreateCmd(repoFlag *string) *cobra.Command {
	var host string
	var asDir bool

	cmd := &cobra.Command{
		Use:   "create [--dir] [--host H] <path...>",
		Short: "Create and track empty files or directories",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if asDir {
				return nil
			}
			if err := validateCreateArgs(args); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			app := svc(repoFlag)

			dirMode := asDir || isDirArg(args[0])
			if err := app.Create(cmd.Context(), host, args, service.CreateOptions{AsDir: dirMode}); err != nil {
				return err
			}

			kind := "file(s)"
			if dirMode {
				kind = "dir(s)"
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Created and tracked %d %s in %s scope\n", len(args), kind, service.NormalizeHost(host))
			return err
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "create paths in a host-specific scope")
	cmd.Flags().BoolVar(&asDir, "dir", false, "create directories instead of files")
	return cmd
}

// validateCreateArgs ensures all positional arguments agree on file vs directory
// semantics when --dir is not set.
func validateCreateArgs(args []string) error {
	sawFile := false
	sawDir := false
	for _, arg := range args {
		if isDirArg(arg) {
			sawDir = true
		} else {
			sawFile = true
		}
		if sawFile && sawDir {
			return lnkerror.WithSuggestion(lnkerror.ErrMixedCreateTypes, "use --dir when creating directories, or run separate commands for files and directories")
		}
	}
	return nil
}

// isDirArg reports whether arg uses a trailing path separator to indicate a
// directory, allowing both Unix and Windows separators.
func isDirArg(arg string) bool {
	return arg != "" && (strings.HasSuffix(arg, "/") || strings.HasSuffix(arg, "\\"))
}
