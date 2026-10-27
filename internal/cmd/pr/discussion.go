package pr

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

func newCmdCommits(f *factory.Factory) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "commits [<pr>]",
		Short:   "Commits del PR (más recientes primero) con mensaje completo",
		Example: "  $ prismatic-rainbow pr commits 42 -o json",
		Args:    cmdutil.MaxArgs(1, "pr commits "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			cs, err := client.ListPullRequestCommits(t.Workspace, t.Repo, t.ID, limit)
			if err != nil {
				return err
			}
			v := newCommitViews(cs)
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			tp := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			tp.AddHeader("COMMIT", "AUTOR", "FECHA", "MENSAJE")
			for _, c := range v {
				tp.AddRow(cmdutil.ShortHash(c.Hash), c.Author, c.Date.Format("2006-01-02 15:04"), cmdutil.Truncate(cmdutil.FirstLine(c.Message), 80))
			}
			return tp.Render()
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "L", 0, "Máximo de commits (0 = todos)")
	return cmd
}

func newCmdComments(f *factory.Factory) *cobra.Command {
	var unresolved, inlineOnly bool
	cmd := &cobra.Command{
		Use:   "comments [<pr>]",
		Short: "Comentarios del PR agrupados en hilos, con anclaje archivo/línea y estado resuelto",
		Example: `  $ prismatic-rainbow pr comments 42
  $ prismatic-rainbow pr comments 42 --unresolved --inline -o json`,
		Args: cmdutil.MaxArgs(1, "pr comments "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			cs, err := client.ListPullRequestComments(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			v := newThreadsView(cs)
			if unresolved || inlineOnly {
				kept := v.Threads[:0]
				for _, th := range v.Threads {
					if (unresolved && th.Resolved) || (inlineOnly && th.Path == "") {
						continue
					}
					kept = append(kept, th)
				}
				v.Threads = kept
				v.Total = len(kept)
				v.Unresolved = 0
				for _, th := range kept {
					if !th.Resolved {
						v.Unresolved++
					}
				}
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			writeThreadsText(f, v)
			return nil
		},
	}
	cmd.Flags().BoolVar(&unresolved, "unresolved", false, "Solo hilos sin resolver")
	cmd.Flags().BoolVar(&inlineOnly, "inline", false, "Solo hilos anclados a un archivo/línea")
	return cmd
}

func writeThreadsText(f *factory.Factory, v ThreadsView) {
	out := f.IO.Out
	fmt.Fprintf(out, "%d hilos · %d sin resolver\n", v.Total, v.Unresolved)
	for _, th := range v.Threads {
		fmt.Fprintln(out)
		anchor := "general"
		if th.Path != "" {
			anchor = th.Path
			if th.Line != nil {
				anchor += fmt.Sprintf(":%d", *th.Line)
			} else if th.FromLine != nil {
				anchor += fmt.Sprintf(":-%d (línea eliminada)", *th.FromLine)
			}
		}
		state := "abierto"
		if th.Resolved {
			state = "resuelto por " + th.ResolvedBy
		}
		fmt.Fprintf(out, "%s [%s] %s\n", f.IO.Bold(fmt.Sprintf("hilo %d", th.ID)), anchor, state)
		for i, c := range th.Comments {
			prefix := "  "
			if i > 0 {
				prefix = "    ↳ "
			}
			body := strings.ReplaceAll(strings.TrimSpace(c.Body), "\n", "\n"+strings.Repeat(" ", len([]rune(prefix))))
			fmt.Fprintf(out, "%s%s (%s): %s\n", prefix, c.Author, c.CreatedOn.Format("2006-01-02 15:04"), body)
		}
	}
}

func newCmdTasks(f *factory.Factory) *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:     "tasks [<pr>]",
		Short:   "Tareas (to-dos bloqueantes) del PR",
		Example: "  $ prismatic-rainbow pr tasks 42 --state unresolved -o json",
		Args:    cmdutil.MaxArgs(1, "pr tasks "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			state = strings.ToUpper(state)
			if state != "" && state != "RESOLVED" && state != "UNRESOLVED" {
				return cmdutil.FlagErrorf("--state inválido: %q (resolved o unresolved)", state)
			}
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			ts, err := client.ListPullRequestTasks(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			if state != "" {
				kept := ts[:0]
				for _, task := range ts {
					if task.State == state {
						kept = append(kept, task)
					}
				}
				ts = kept
			}
			v := newTasksView(ts)
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			tp := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			tp.AddHeader("ID", "ESTADO", "TAREA")
			for _, task := range v.Tasks {
				tp.AddRow(itoa(task.ID), task.State, cmdutil.Truncate(cmdutil.FirstLine(task.Body), 90))
			}
			return tp.Render()
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "Filtrar: resolved o unresolved")
	return cmd
}
