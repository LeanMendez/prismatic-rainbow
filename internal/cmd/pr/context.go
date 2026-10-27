package pr

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/diffparse"
	"github.com/LeanMendez/prismatic-rainbow/internal/tasklink"
)

// Warning reports a section that could not be loaded; the rest of the
// bundle is still returned.
type Warning struct {
	Section string `json:"section"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ContextView bundles everything an agent needs to judge a PR in a single
// call: metadata, review state, checks, mergeability, changed files,
// commits, discussion, PR tasks, ClickUp references and optionally the diff.
type ContextView struct {
	Repository     string               `json:"repository"`
	PullRequest    PRView               `json:"pull_request"`
	Checks         *ChecksView          `json:"checks"`
	Mergeability   *MergeabilityView    `json:"mergeability"`
	Files          *FilesView           `json:"files"`
	Commits        []CommitView         `json:"commits"`
	Threads        *ThreadsView         `json:"threads"`
	Tasks          *TasksView           `json:"tasks"`
	TaskReferences []tasklink.Reference `json:"task_references"`
	Diff           *DiffView            `json:"diff,omitempty"`
	Warnings       []Warning            `json:"warnings"`
}

func newCmdContext(f *factory.Factory) *cobra.Command {
	var d DiffFlags
	var withDiff bool
	var prefixes []string
	cmd := &cobra.Command{
		Use:   "context [<pr>]",
		Short: "Todo el contexto de un PR en una sola llamada (para agentes de review/QA)",
		Long: `Arma un único documento con: PR normalizado (estado, ramas, commits, revisores),
checks de build, mergeability, archivos cambiados, commits, hilos de comentarios,
tareas del PR y referencias a tareas de ClickUp. Con --diff incluye además el
diff acotado por --max-bytes (default 200000) y --max-lines-per-file.

Las secciones que fallan (p.ej. falta un scope) no abortan: quedan en null y se
reportan en warnings. Texto plano = Markdown; -o json = contrato estable.`,
		Example: `  $ prismatic-rainbow pr context 42 -o json
  $ prismatic-rainbow pr context 42 --diff --exclude '*.lock' --max-lines-per-file 300 -o json
  $ prismatic-rainbow pr context            # PR abierto de la rama actual, en Markdown`,
		Args: cmdutil.MaxArgs(1, "pr context "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			var df *DiffFlags
			if withDiff {
				df = &d
			}
			v, err := buildContext(client, t, df, customPrefixes(f, prefixes))
			if err != nil {
				return err
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			writeContextMarkdown(f.IO.Out, v)
			return nil
		},
	}
	cmd.Flags().BoolVar(&withDiff, "diff", false, "Incluir el diff (acotado)")
	d.register(cmd, 200000)
	cmd.Flags().StringArrayVar(&prefixes, "custom-prefix", nil, "Prefijo de IDs custom de ClickUp (repetible)")
	return cmd
}

func buildContext(client *api.Client, t *cmdutil.PRTarget, d *DiffFlags, prefixes []string) (*ContextView, error) {
	p, err := client.GetPullRequest(t.Workspace, t.Repo, t.ID)
	if err != nil {
		return nil, err
	}
	v := &ContextView{
		Repository:  t.Workspace + "/" + t.Repo,
		PullRequest: newPRView(p),
		Commits:     []CommitView{},
		Warnings:    []Warning{},
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		commits  []api.Commit
		comments []api.Comment
	)
	warn := func(section string, err error) {
		w := Warning{Section: section, Code: "error", Message: err.Error()}
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			w.Code = apiErr.Code()
		}
		mu.Lock()
		v.Warnings = append(v.Warnings, w)
		mu.Unlock()
	}
	run := func(section string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				warn(section, err)
			}
		}()
	}
	ws, repo, id := t.Workspace, t.Repo, t.ID

	run("checks", func() error {
		ss, err := client.ListPullRequestStatuses(ws, repo, id)
		if err == nil {
			cv := newChecksView(ss)
			v.Checks = &cv
		}
		return err
	})
	run("mergeability", func() error {
		cs, err := client.GetMergeabilityChecks(ws, repo, id)
		if err == nil {
			mv := newMergeabilityView(cs)
			v.Mergeability = &mv
		}
		return err
	})
	run("files", func() error {
		st, err := client.GetPullRequestDiffStat(ws, repo, id)
		if err == nil {
			fv := newFilesView(st)
			v.Files = &fv
		}
		return err
	})
	run("commits", func() error {
		cs, err := client.ListPullRequestCommits(ws, repo, id, 0)
		if err == nil {
			commits = cs
			v.Commits = newCommitViews(cs)
		}
		return err
	})
	run("threads", func() error {
		cs, err := client.ListPullRequestComments(ws, repo, id)
		if err == nil {
			comments = cs
			tv := newThreadsView(cs)
			v.Threads = &tv
		}
		return err
	})
	run("tasks", func() error {
		ts, err := client.ListPullRequestTasks(ws, repo, id)
		if err == nil {
			tv := newTasksView(ts)
			v.Tasks = &tv
		}
		return err
	})
	if d != nil {
		run("diff", func() error {
			dv, err := fetchDiff(client, t, *d)
			if err == nil {
				v.Diff = dv
			}
			return err
		})
	}
	wg.Wait()

	v.TaskReferences = tasklink.Extract(prTexts(p, commits, comments), prefixes)
	if v.TaskReferences == nil {
		v.TaskReferences = []tasklink.Reference{}
	}
	return v, nil
}

func writeContextMarkdown(w io.Writer, v *ContextView) {
	p := v.PullRequest
	state := p.State
	if p.Draft {
		state += " (draft)"
	}
	fmt.Fprintf(w, "# PR #%d: %s\n\n", p.ID, p.Title)
	fmt.Fprintf(w, "- Repo: %s · %s\n", v.Repository, p.URL)
	fmt.Fprintf(w, "- Estado: %s · autor: %s · creado %s · actualizado %s\n", state, p.Author,
		p.CreatedOn.Format("2006-01-02"), p.UpdatedOn.Format("2006-01-02"))
	fmt.Fprintf(w, "- Ramas: %s (%s) → %s (%s)\n", p.Source.Branch, cmdutil.ShortHash(p.Source.Commit),
		p.Destination.Branch, cmdutil.ShortHash(p.Destination.Commit))
	fmt.Fprintf(w, "- Revisión: %d aprobaciones, %d cambios pedidos", p.Review.Approvals, p.Review.ChangesRequested)
	for _, r := range p.Review.Reviewers {
		fmt.Fprintf(w, " · %s [%s]", r.Name, r.State)
	}
	fmt.Fprintln(w)
	if v.Checks != nil {
		fmt.Fprintf(w, "- Checks: %s (%d ok, %d fallidos, %d en curso)\n", v.Checks.Overall, v.Checks.Passed, v.Checks.Failed, v.Checks.Pending)
	}
	if v.Mergeability != nil {
		blocking := "nada"
		if len(v.Mergeability.Blocking) > 0 {
			blocking = strings.Join(v.Mergeability.Blocking, ", ")
		}
		fmt.Fprintf(w, "- Mergeable: %t · bloquea: %s\n", v.Mergeability.Mergeable, blocking)
	}

	fmt.Fprintf(w, "\n## Tareas de ClickUp referenciadas\n\n")
	if len(v.TaskReferences) == 0 {
		fmt.Fprintln(w, "Ninguna detectada.")
	}
	for _, r := range v.TaskReferences {
		fmt.Fprintf(w, "- %s (%s, confianza %s) en: %s", r.ID, r.Kind, r.Confidence, sourcesSummary(r))
		if r.StatusHint != "" {
			fmt.Fprintf(w, " · estado pedido: %s", r.StatusHint)
		}
		fmt.Fprintln(w)
	}

	if p.Description != "" {
		fmt.Fprintf(w, "\n## Descripción\n\n%s\n", strings.TrimSpace(p.Description))
	}

	if v.Files != nil {
		fmt.Fprintf(w, "\n## Archivos (%d · +%d -%d)\n\n", v.Files.Count, v.Files.LinesAdded, v.Files.LinesRemoved)
		for _, fs := range v.Files.Files {
			name := fs.Path
			if fs.OldPath != "" {
				name = fs.OldPath + " → " + fs.Path
			}
			fmt.Fprintf(w, "- %s %s (+%d -%d)\n", fs.Status, name, fs.LinesAdded, fs.LinesRemoved)
		}
	}

	if len(v.Commits) > 0 {
		fmt.Fprintf(w, "\n## Commits (%d)\n\n", len(v.Commits))
		for _, c := range v.Commits {
			fmt.Fprintf(w, "- %s %s — %s\n", cmdutil.ShortHash(c.Hash), cmdutil.FirstLine(c.Message), c.Author)
		}
	}

	if v.Checks != nil && len(v.Checks.Statuses) > 0 {
		fmt.Fprintf(w, "\n## Checks\n\n")
		for _, s := range v.Checks.Statuses {
			fmt.Fprintf(w, "- %s: %s", s.Bucket, s.Name)
			if s.Description != "" {
				fmt.Fprintf(w, " — %s", s.Description)
			}
			fmt.Fprintln(w)
		}
	}

	if v.Threads != nil {
		fmt.Fprintf(w, "\n## Hilos sin resolver (%d de %d)\n\n", v.Threads.Unresolved, v.Threads.Total)
		for _, th := range v.Threads.Threads {
			if th.Resolved {
				continue
			}
			anchor := "general"
			if th.Path != "" {
				anchor = th.Path
				if th.Line != nil {
					anchor += fmt.Sprintf(":%d", *th.Line)
				}
			}
			first := th.Comments[0]
			fmt.Fprintf(w, "- [%s] %s: %s", anchor, first.Author, cmdutil.Truncate(strings.ReplaceAll(strings.TrimSpace(first.Body), "\n", " "), 200))
			if n := len(th.Comments) - 1; n > 0 {
				fmt.Fprintf(w, " (+%d respuestas)", n)
			}
			fmt.Fprintln(w)
		}
	}

	if v.Tasks != nil && v.Tasks.Total > 0 {
		fmt.Fprintf(w, "\n## Tareas del PR (%d sin resolver de %d)\n\n", v.Tasks.Unresolved, v.Tasks.Total)
		for _, t := range v.Tasks.Tasks {
			box := " "
			if t.State == api.TaskResolved {
				box = "x"
			}
			fmt.Fprintf(w, "- [%s] %s\n", box, cmdutil.FirstLine(t.Body))
		}
	}

	if v.Diff != nil {
		fmt.Fprintf(w, "\n## Diff\n\n```diff\n%s```\n", diffparse.Join(v.Diff.Files))
		if len(v.Diff.OmittedFiles) > 0 {
			fmt.Fprintf(w, "\nPatches omitidos por tamaño: %s\n", strings.Join(v.Diff.OmittedFiles, ", "))
		}
	}

	if len(v.Warnings) > 0 {
		fmt.Fprintf(w, "\n## Avisos\n\n")
		for _, wr := range v.Warnings {
			fmt.Fprintf(w, "- %s: %s\n", wr.Section, wr.Message)
		}
	}
}
