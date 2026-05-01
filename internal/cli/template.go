package cli

import "github.com/spf13/cobra"

func newTemplateCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"templates"},
		Short:   "Work with Contaigen template files",
		Long: `Validate and manage Contaigen template files.

Templates include environment profiles and service templates. Validation is
strict so misspelled YAML fields fail loudly before they can create surprising
Docker resources.`,
		Example: `  contaigen template validate ./profiles/kali-web.yaml
  contaigen template validate ./services/juice-shop.yaml`,
	}

	cmd.AddCommand(newTemplateValidateCommand(opts))
	return cmd
}

func newTemplateValidateCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a Contaigen template file",
		Example: `  contaigen template validate ./profiles/kali-web.yaml
  contaigen template validate ./services/juice-shop.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := opts.NewProfileStore()
			if err != nil {
				return err
			}
			template, err := profiles.ValidateAnyFile(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printSuccess(cmd, "Valid %s %s", template.Kind, template.Name)
			return nil
		},
	}
}
