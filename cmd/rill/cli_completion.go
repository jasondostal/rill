package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func addCompletionCommand(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:    "completion [bash|zsh|fish|powershell]",
		Short:  "Generate shell completion script",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletion(os.Stdout)
			}
			return fmt.Errorf("unknown shell: %s", args[0])
		},
	})
}
