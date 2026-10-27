package pr

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/iostreams"
)

func newCmdView(f *factory.Factory) *cobra.Command {
	var web bool
	cmd := &cobra.Command{
		Use:   "view [<pr>]",
		Short: "Muestra un PR: estado, ramas, commits de origen/destino, revisores y descripción",
		Example: `  $ prismatic-rainbow pr view 42
  $ prismatic-rainbow pr view https://bitbucket.org/empresa/api/pull-requests/42 -o json
  $ prismatic-rainbow pr view            # PR abierto de la rama actual`,
		Args: cmdutil.MaxArgs(1, "pr view "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			p, err := client.GetPullRequest(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			v := newPRView(p)
			if web {
				return openBrowser(v.URL)
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			writePRText(f.IO, f.IO.Out, v)
			return nil
		},
	}
	cmd.Flags().BoolVar(&web, "web", false, "Abrir el PR en el navegador")
	return cmd
}

// writePRText renders a PR as Markdown (readable by humans and LLMs).
func writePRText(ios *iostreams.IOStreams, w io.Writer, v PRView) {
	state := v.State
	if v.Draft {
		state += " (draft)"
	}
	fmt.Fprintf(w, "%s %s\n", ios.Bold(fmt.Sprintf("#%d", v.ID)), v.Title)
	fmt.Fprintf(w, "%s · %s → %s · autor: %s\n", state, v.Source.Branch, v.Destination.Branch, v.Author)
	fmt.Fprintf(w, "commits: origen %s · destino %s", cmdutil.ShortHash(v.Source.Commit), cmdutil.ShortHash(v.Destination.Commit))
	if v.MergeCommit != "" {
		fmt.Fprintf(w, " · merge %s", cmdutil.ShortHash(v.MergeCommit))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "creado %s · actualizado %s · comentarios %d · tareas %d\n",
		v.CreatedOn.Format("2006-01-02 15:04"), v.UpdatedOn.Format("2006-01-02 15:04"), v.CommentCount, v.TaskCount)
	if v.ClosedBy != "" {
		fmt.Fprintf(w, "cerrado por %s", v.ClosedBy)
		if v.Reason != "" {
			fmt.Fprintf(w, " (%s)", v.Reason)
		}
		fmt.Fprintln(w)
	}
	var revs []string
	for _, r := range v.Review.Reviewers {
		revs = append(revs, fmt.Sprintf("%s [%s]", r.Name, r.State))
	}
	fmt.Fprintf(w, "aprobaciones %d · cambios pedidos %d", v.Review.Approvals, v.Review.ChangesRequested)
	if len(revs) > 0 {
		fmt.Fprintf(w, " · %s", strings.Join(revs, ", "))
	}
	fmt.Fprintf(w, "\n%s\n", v.URL)
	if v.Description != "" {
		fmt.Fprintf(w, "\n%s\n", v.Description)
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
