// Package repo implements `repo list` and `repo view`.
package repo

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

// RepoView is the JSON shape of a repository.
type RepoView struct {
	FullName    string `json:"full_name"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	MainBranch  string `json:"main_branch,omitempty"`
	UpdatedOn   string `json:"updated_on"`
	URL         string `json:"url"`
}

// NewCmdRepo groups repo subcommands.
func NewCmdRepo(f *factory.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "repo", Short: "Repositorios del workspace"}
	cmd.AddCommand(newCmdList(f), newCmdView(f))
	return cmd
}

func newCmdList(f *factory.Factory) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "Lista repos del workspace (más recientes primero)",
		Example: "  $ prismatic-rainbow repo list -w empresa --limit 50 -o json",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			client, err := f.Client()
			if err != nil {
				return err
			}
			ws, err := cmdutil.ResolveWorkspace(f)
			if err != nil {
				return err
			}
			repos, err := client.ListRepositories(ws, limit)
			if err != nil {
				return err
			}
			views := make([]RepoView, len(repos))
			for i, r := range repos {
				views[i] = RepoView{r.FullName, r.Slug, r.Name, r.Description, r.IsPrivate, r.MainBranch(), r.UpdatedOn.Format("2006-01-02T15:04:05Z07:00"), r.Links.HTML.Href}
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, views)
			}
			t := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			t.AddHeader("REPO", "RAMA PRINCIPAL", "ACTUALIZADO", "DESCRIPCIÓN")
			for i, v := range views {
				t.AddRow(v.FullName, v.MainBranch, cmdutil.Ago(repos[i].UpdatedOn), cmdutil.Truncate(v.Description, 50))
			}
			return t.Render()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 30, "Máximo de repos (0 = todos)")
	return cmd
}

func newCmdView(f *factory.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Muestra el repo actual (o --repo)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			client, err := f.Client()
			if err != nil {
				return err
			}
			ws, slug, err := cmdutil.ResolveRepo(f)
			if err != nil {
				return err
			}
			r, err := client.GetRepository(ws, slug)
			if err != nil {
				return err
			}
			v := RepoView{r.FullName, r.Slug, r.Name, r.Description, r.IsPrivate, r.MainBranch(), r.UpdatedOn.Format("2006-01-02T15:04:05Z07:00"), r.Links.HTML.Href}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			fmt.Fprintf(f.IO.Out, "%s\nRama principal: %s · Privado: %t\n%s\n", f.IO.Bold(v.FullName), v.MainBranch, v.Private, v.URL)
			if v.Description != "" {
				fmt.Fprintf(f.IO.Out, "\n%s\n", v.Description)
			}
			return nil
		},
	}
}
