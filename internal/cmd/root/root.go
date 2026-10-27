// Package root assembles the command tree and global flags.
package root

import (
	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/apicmd"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/auth"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/branch"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/pr"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/repo"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/src"
)

// NewCmdRoot builds the root command around f. f.Options is bound to the
// persistent flags.
func NewCmdRoot(f *factory.Factory) *cobra.Command {
	opts := f.Options
	cmd := &cobra.Command{
		Use:   "prismatic-rainbow <comando> <subcomando> [flags]",
		Short: "CLI de Bitbucket Cloud pensada para agentes: PRs, diffs, código y estado",
		Long: `prismatic-rainbow consulta Bitbucket Cloud (API 2.0) desde la terminal.

Pensada para agentes de código y de QA: toda lectura soporta --output json con
un contrato estable, los diffs se pueden filtrar y acotar, se puede leer el
código de cualquier archivo en la rama del PR, y 'pr context' arma en una sola
llamada todo lo necesario para contrastar un PR con su tarea de ClickUp.`,
		Example: `  $ prismatic-rainbow pr list -o json
  $ prismatic-rainbow pr context 42 --diff -o json
  $ prismatic-rainbow pr diff 42 --exclude '*.lock' --max-lines-per-file 300
  $ prismatic-rainbow pr file 42 src/app.ts
  $ prismatic-rainbow pr links 42 -o json`,
		Version:           f.Version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		DisableAutoGenTag: true,
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &cmdutil.FlagError{Err: err} })

	pf := cmd.PersistentFlags()
	pf.StringVar(&opts.Email, "email", "", "Email de Atlassian (o BITBUCKET_EMAIL); vacío = token Bearer")
	pf.StringVar(&opts.Token, "token", "", "API token / access token (o BITBUCKET_TOKEN)")
	pf.StringVarP(&opts.Workspace, "workspace", "w", "", "Workspace (por defecto: remote git, BITBUCKET_WORKSPACE o config)")
	pf.StringVarP(&opts.Repo, "repo", "R", "", "Repo slug o 'workspace/repo' (por defecto: remote git o BITBUCKET_REPO_SLUG)")
	pf.StringVarP(&opts.Output, "output", "o", "", `Formato de salida: "" (texto) o "json"`)

	cmd.PersistentPreRunE = func(*cobra.Command, []string) error {
		if opts.Output != "" && opts.Output != "json" {
			return cmdutil.FlagErrorf("--output inválido: %q (soportado: json)", opts.Output)
		}
		return nil
	}

	cmd.AddCommand(
		auth.NewCmdAuth(f),
		repo.NewCmdRepo(f),
		branch.NewCmdBranch(f),
		src.NewCmdSrc(f),
		pr.NewCmdPR(f),
		apicmd.NewCmdAPI(f),
	)
	return cmd
}
