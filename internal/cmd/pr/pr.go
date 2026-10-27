// Package pr implements the pull request commands. Read commands share the
// normalized JSON contract in model.go.
package pr

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
)

// NewCmdPR groups the pr subcommands.
func NewCmdPR(f *factory.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull requests: estado, diffs, código, comentarios, checks y vínculos con ClickUp",
		Long: `Comandos de pull requests.

<pr> acepta un número (42 o #42) o la URL del PR
(https://bitbucket.org/<ws>/<repo>/pull-requests/42). Si se omite, se usa el PR
abierto cuya rama origen es la rama git actual.`,
	}
	cmd.AddCommand(
		newCmdList(f),
		newCmdView(f),
		newCmdContext(f),
		newCmdDiff(f),
		newCmdFiles(f),
		newCmdFile(f),
		newCmdCommits(f),
		newCmdComments(f),
		newCmdTasks(f),
		newCmdChecks(f),
		newCmdMergeability(f),
		newCmdLinks(f),
		newCmdCreate(f),
		newCmdComment(f),
		newCmdApprove(f),
		newCmdRequestChanges(f),
		newCmdMerge(f),
	)
	return cmd
}

const prArgUsage = "acepta como máximo un argumento: el número o la URL del PR"

// resolve returns an authenticated client and the target PR from the
// optional positional argument.
func resolve(f *factory.Factory, args []string) (*api.Client, *cmdutil.PRTarget, error) {
	client, err := f.Client()
	if err != nil {
		return nil, nil, err
	}
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	t, err := cmdutil.ResolvePR(f, client, arg)
	if err != nil {
		return nil, nil, err
	}
	return client, t, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
