package pr

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

var allStates = []string{api.StateOpen, api.StateMerged, api.StateDeclined, api.StateSuperseded}

// ListOptions are the `pr list` filters.
type ListOptions struct {
	States []string
	Source string
	Dest   string
	Search string
	Task   string
	Query  string
	Mine   bool
	Limit  int
}

// parseStates accepts OPEN,MERGED,… (case-insensitive) or "all".
func parseStates(in []string) ([]string, error) {
	var out []string
	for _, raw := range in {
		for _, s := range strings.Split(raw, ",") {
			s = strings.ToUpper(strings.TrimSpace(s))
			switch {
			case s == "":
			case s == "ALL":
				return allStates, nil
			case contains(allStates, s):
				out = append(out, s)
			default:
				return nil, cmdutil.FlagErrorf("--state inválido: %q (OPEN, MERGED, DECLINED, SUPERSEDED o all)", s)
			}
		}
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func bbqlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// buildQuery turns the filters into one BBQL expression.
func buildQuery(o ListOptions, userUUID string) string {
	var parts []string
	if o.Source != "" {
		parts = append(parts, "source.branch.name = "+bbqlString(o.Source))
	}
	if o.Dest != "" {
		parts = append(parts, "destination.branch.name = "+bbqlString(o.Dest))
	}
	if o.Search != "" {
		parts = append(parts, "title ~ "+bbqlString(o.Search))
	}
	if o.Task != "" {
		t := bbqlString(o.Task)
		parts = append(parts, fmt.Sprintf("(title ~ %s OR source.branch.name ~ %s OR description ~ %s)", t, t, t))
	}
	if userUUID != "" {
		parts = append(parts, "author.uuid = "+bbqlString(userUUID))
	}
	if o.Query != "" {
		parts = append(parts, "("+o.Query+")")
	}
	return strings.Join(parts, " AND ")
}

// ListItem is a compact PR row. The list endpoint does not return
// participants, so review state is only in `pr view` / `pr context`.
type ListItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Draft       bool   `json:"draft"`
	Author      string `json:"author"`
	Source      string `json:"source_branch"`
	Destination string `json:"destination_branch"`
	SourceHash  string `json:"source_commit,omitempty"`
	Comments    int    `json:"comment_count"`
	Tasks       int    `json:"task_count"`
	CreatedOn   string `json:"created_on"`
	UpdatedOn   string `json:"updated_on"`
	URL         string `json:"url"`
}

func newCmdList(f *factory.Factory) *cobra.Command {
	var o ListOptions
	var states []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista pull requests",
		Example: `  $ prismatic-rainbow pr list
  $ prismatic-rainbow pr list --state all --task CU-86b1x2y3z -o json
  $ prismatic-rainbow pr list --state merged --dest develop --limit 100
  $ prismatic-rainbow pr list --mine
  $ prismatic-rainbow pr list --query 'updated_on > 2026-10-01'`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			var err error
			if o.States, err = parseStates(states); err != nil {
				return err
			}
			client, err := f.Client()
			if err != nil {
				return err
			}
			ws, repo, err := cmdutil.ResolveRepo(f)
			if err != nil {
				return err
			}
			uuid := ""
			if o.Mine {
				u, err := client.CurrentUser()
				if err != nil {
					return fmt.Errorf("--mine necesita un token de usuario: %w", err)
				}
				uuid = u.UUID
			}
			prs, err := client.ListPullRequests(ws, repo, api.ListPullRequestsOptions{
				States: o.States, Query: buildQuery(o, uuid), Sort: "-updated_on", Limit: o.Limit,
			})
			if err != nil {
				return err
			}
			items := make([]ListItem, len(prs))
			for i := range prs {
				p := &prs[i]
				items[i] = ListItem{p.ID, p.Title, p.State, p.Draft, p.Author.Name(), p.Source.Branch.Name,
					p.Destination.Branch.Name, p.Source.CommitHash(),
					p.CommentCount, p.TaskCount, p.CreatedOn.Format(timeFmt), p.UpdatedOn.Format(timeFmt), p.Links.HTML.Href}
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, items)
			}
			t := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			t.AddHeader("ID", "TÍTULO", "RAMAS", "AUTOR", "ESTADO", "ACTUALIZADO")
			for i, it := range items {
				state := it.State
				if it.Draft {
					state += " (draft)"
				}
				t.AddRow(itoa(it.ID), cmdutil.Truncate(it.Title, 60), it.Source+" → "+it.Destination,
					it.Author, state, cmdutil.Ago(prs[i].UpdatedOn))
			}
			return t.Render()
		},
	}
	cmd.Flags().StringSliceVarP(&states, "state", "s", []string{api.StateOpen}, "Estados: OPEN, MERGED, DECLINED, SUPERSEDED o all (separados por coma)")
	cmd.Flags().StringVar(&o.Source, "source", "", "Rama origen exacta")
	cmd.Flags().StringVar(&o.Dest, "dest", "", "Rama destino exacta")
	cmd.Flags().StringVar(&o.Search, "search", "", "Texto contenido en el título")
	cmd.Flags().StringVar(&o.Task, "task", "", "ID de tarea (p.ej. CU-86b1x2y3z o ENG-123) en título, rama o descripción")
	cmd.Flags().StringVar(&o.Query, "query", "", "Expresión BBQL adicional (se combina con AND)")
	cmd.Flags().BoolVar(&o.Mine, "mine", false, "Solo PRs creados por el usuario autenticado")
	cmd.Flags().IntVarP(&o.Limit, "limit", "L", 30, "Máximo de PRs (0 = todos)")
	return cmd
}

const timeFmt = "2006-01-02T15:04:05Z07:00"
