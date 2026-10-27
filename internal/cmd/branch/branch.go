// Package branch implements `branch list`.
package branch

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

// BranchView is the JSON shape of a branch.
type BranchView struct {
	Name          string    `json:"name"`
	Commit        string    `json:"commit"`
	CommitDate    time.Time `json:"commit_date"`
	CommitAuthor  string    `json:"commit_author"`
	CommitMessage string    `json:"commit_message"`
}

// NewCmdBranch groups branch subcommands.
func NewCmdBranch(f *factory.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "branch", Short: "Ramas del repositorio"}
	var limit int
	var match string
	list := &cobra.Command{
		Use:   "list",
		Short: "Lista ramas (último commit primero)",
		Example: `  $ prismatic-rainbow branch list --match CU-86b1x2
  $ prismatic-rainbow branch list -R empresa/api --limit 0 -o json`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			client, err := f.Client()
			if err != nil {
				return err
			}
			ws, repo, err := cmdutil.ResolveRepo(f)
			if err != nil {
				return err
			}
			q := ""
			if match != "" {
				q = fmt.Sprintf("name ~ %q", match)
			}
			branches, err := client.ListBranches(ws, repo, q, limit)
			if err != nil {
				return err
			}
			views := make([]BranchView, len(branches))
			for i, b := range branches {
				author := b.Target.Author.Raw
				if b.Target.Author.User != nil {
					author = b.Target.Author.User.Name()
				}
				views[i] = BranchView{b.Name, b.Target.Hash, b.Target.Date, author, cmdutil.FirstLine(b.Target.Message)}
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, views)
			}
			t := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			t.AddHeader("RAMA", "COMMIT", "AUTOR", "FECHA")
			for _, v := range views {
				t.AddRow(v.Name, cmdutil.ShortHash(v.Commit), v.CommitAuthor, cmdutil.Ago(v.CommitDate))
			}
			return t.Render()
		},
	}
	list.Flags().IntVar(&limit, "limit", 30, "Máximo de ramas (0 = todas)")
	list.Flags().StringVar(&match, "match", "", "Filtrar ramas cuyo nombre contenga este texto (p.ej. un ID de ClickUp)")
	cmd.AddCommand(list)
	return cmd
}
