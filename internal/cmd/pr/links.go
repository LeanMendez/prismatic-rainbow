package pr

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
	"github.com/LeanMendez/prismatic-rainbow/internal/tasklink"
)

// customPrefixes merges --custom-prefix with the configured prefixes.
func customPrefixes(f *factory.Factory, flags []string) []string {
	out := append([]string{}, flags...)
	if cfg, err := f.Config(); err == nil {
		out = append(out, cfg.ClickUpCustomIDPrefixes...)
	}
	return out
}

// LinksView is the `pr links` output.
type LinksView struct {
	PullRequest struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Branch string `json:"source_branch"`
		URL    string `json:"url"`
	} `json:"pull_request"`
	References []tasklink.Reference `json:"references"`
}

func newCmdLinks(f *factory.Factory) *cobra.Command {
	var prefixes []string
	var noComments bool
	cmd := &cobra.Command{
		Use:   "links [<pr>]",
		Short: "Tareas de ClickUp referenciadas por el PR (título, rama, descripción, commits, comentarios)",
		Long: `Detecta referencias a tareas de ClickUp con las mismas convenciones que la
integración ClickUp ↔ Bitbucket:

  CU-<id>                 (alta confianza; también el formato de rama CU-<id>_<nombre>)
  https://app.clickup.com/t/<id>  y  /t/<team>/<CUSTOM-ID>   (alta)
  <PREFIJO>-<n>           IDs custom, solo para prefijos configurados (alta)
  #<id>                   (baja confianza: '#' también se usa para otras cosas)

Un sufijo [estado] (CU-abc[ready]) es la sintaxis de ClickUp para cambiar el
estado y se reporta en status_hint. Los prefijos custom se configuran con
--custom-prefix, CLICKUP_CUSTOM_ID_PREFIXES=ENG,OPS o
clickup_custom_id_prefixes en el config.`,
		Example: `  $ prismatic-rainbow pr links 42 -o json
  $ prismatic-rainbow pr links 42 --custom-prefix ENG --custom-prefix OPS`,
		Args: cmdutil.MaxArgs(1, "pr links "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			p, err := client.GetPullRequest(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			commits, err := client.ListPullRequestCommits(t.Workspace, t.Repo, t.ID, 0)
			if err != nil {
				return err
			}
			var comments []api.Comment
			if !noComments {
				if comments, err = client.ListPullRequestComments(t.Workspace, t.Repo, t.ID); err != nil {
					return err
				}
			}
			var v LinksView
			v.PullRequest.ID, v.PullRequest.Title, v.PullRequest.State = p.ID, p.Title, p.State
			v.PullRequest.Branch, v.PullRequest.URL = p.Source.Branch.Name, p.Links.HTML.Href
			v.References = tasklink.Extract(prTexts(p, commits, comments), customPrefixes(f, prefixes))
			if v.References == nil {
				v.References = []tasklink.Reference{}
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			if len(v.References) == 0 {
				fmt.Fprintln(f.IO.Out, "No se encontraron referencias a tareas de ClickUp.")
				return nil
			}
			tp := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			tp.AddHeader("TAREA", "CONFIANZA", "ESTADO", "ENCONTRADA EN")
			for _, r := range v.References {
				tp.AddRow(r.ID, r.Confidence, r.StatusHint, sourcesSummary(r))
			}
			return tp.Render()
		},
	}
	cmd.Flags().StringArrayVar(&prefixes, "custom-prefix", nil, "Prefijo de IDs custom de ClickUp (p.ej. ENG para ENG-123; repetible)")
	cmd.Flags().BoolVar(&noComments, "no-comments", false, "No escanear comentarios del PR (una llamada menos)")
	return cmd
}

func sourcesSummary(r tasklink.Reference) string {
	seen := map[string]bool{}
	var out []string
	for _, l := range r.FoundIn {
		if !seen[l.Source] {
			seen[l.Source] = true
			out = append(out, l.Source)
		}
	}
	return strings.Join(out, ", ")
}
