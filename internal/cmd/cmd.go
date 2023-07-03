package cmd

import (
	"github.com/josebalius/gh-csfs/internal/csfs"
	"github.com/spf13/cobra"
)

var version = "dev"

func New(a *csfs.App) *cobra.Command {
	var codespace string
	var workspace string
	var exclude []string
	var deleteFiles bool
	var watch []string

	cmd := &cobra.Command{
		Use:           "csfs",
		SilenceUsage:  true,  // don't print usage message after each error (see #80)
		SilenceErrors: false, // print errors automatically so that main need not
		Long: `csfs is a command-line utility designed for synchronizing GitHub Codespaces with a local filesystem.

To utilize csfs, the user must set the GITHUB_TOKEN environment variable with an appropriate GitHub API access token. 
Additionally, csfs requires the GitHub command-line tool (gh) and rsync to be installed and configured on the system.`,
		Version: version,

		RunE: func(cmd *cobra.Command, args []string) error {
			opts := csfs.AppOptions{
				Codespace:   codespace,
				Workspace:   workspace,
				Exclude:     exclude,
				DeleteFiles: deleteFiles,
				Watch:       watch,
			}

			return a.Run(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&codespace, "codespace", "c", "", "codespace to use")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace to use")
	cmd.Flags().StringSliceVarP(&exclude, "exclude", "e", []string{}, "exclude files matching pattern")
	cmd.Flags().BoolVarP(&deleteFiles, "delete", "d", false, "delete files that don't exist in the codespace")
	cmd.Flags().StringSliceVarP(&watch, "watch", "W", []string{}, "watch files matching pattern")

	return cmd
}
