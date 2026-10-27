package pr

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
)

// readBody returns --body, or the contents of --body-file ("-" = stdin).
func readBody(f *factory.Factory, body, bodyFile string) (string, error) {
	if bodyFile == "" {
		return body, nil
	}
	if body != "" {
		return "", cmdutil.FlagErrorf("usá --body o --body-file, no ambos")
	}
	var r io.Reader = f.IO.In
	if bodyFile != "-" {
		file, err := os.Open(bodyFile)
		if err != nil {
			return "", err
		}
		defer file.Close()
		r = file
	}
	b, err := io.ReadAll(r)
	return string(b), err
}

func newCmdCreate(f *factory.Factory) *cobra.Command {
	var title, body, bodyFile, source, dest string
	var reviewers []string
	var closeSource, draft bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea un PR (origen: rama actual; destino: rama principal)",
		Example: `  $ prismatic-rainbow pr create --title "CU-86b1x2y3z Login con SSO" --body-file pr.md
  $ prismatic-rainbow pr create --title "Fix" --dest develop --reviewer ana,juan --close-source-branch`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if strings.TrimSpace(title) == "" {
				return cmdutil.FlagErrorf("--title es requerido")
			}
			desc, err := readBody(f, body, bodyFile)
			if err != nil {
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
			if source == "" {
				gc, err := f.GitContext()
				if err != nil || gc.Branch == "" {
					return cmdutil.FlagErrorf("no se pudo inferir la rama origen: usá --source")
				}
				source = gc.Branch
			}
			in := api.CreatePullRequestInput{Title: title, Description: desc, CloseSourceBranch: closeSource, Draft: draft}
			in.Source.Branch.Name = source
			if dest != "" {
				in.Destination = &api.Endpoint{}
				in.Destination.Branch.Name = dest
			}
			if len(reviewers) > 0 {
				accounts, err := resolveReviewers(client, ws, reviewers)
				if err != nil {
					return err
				}
				in.Reviewers = accounts
			}
			p, err := client.CreatePullRequest(ws, repo, in)
			if err != nil {
				return err
			}
			v := newPRView(p)
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			fmt.Fprintf(f.IO.Out, "%s PR #%d creado: %s\n", f.IO.Green("✓"), v.ID, v.URL)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&title, "title", "t", "", "Título (incluí el ID de ClickUp, p.ej. CU-86b1x2y3z, para vincular la tarea)")
	fl.StringVarP(&body, "body", "b", "", "Descripción")
	fl.StringVar(&bodyFile, "body-file", "", `Leer la descripción de un archivo ("-" = stdin)`)
	fl.StringVar(&source, "source", "", "Rama origen (default: rama git actual)")
	fl.StringVar(&dest, "dest", "", "Rama destino (default: rama principal del repo)")
	fl.StringSliceVar(&reviewers, "reviewer", nil, "Revisores: nickname, nombre, account_id o {uuid} (separados por coma)")
	fl.BoolVar(&closeSource, "close-source-branch", false, "Cerrar la rama origen al mergear")
	fl.BoolVar(&draft, "draft", false, "Crear como borrador")
	return cmd
}

func resolveReviewers(client *api.Client, ws string, names []string) ([]api.Account, error) {
	members, err := client.ListWorkspaceMembers(ws)
	if err != nil {
		return nil, fmt.Errorf("listando miembros del workspace para resolver revisores: %w", err)
	}
	var out []api.Account
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		var match []api.Account
		for _, m := range members {
			if strings.EqualFold(m.Nickname, n) || strings.EqualFold(m.DisplayName, n) || m.AccountID == n || m.UUID == n {
				match = append(match, m)
			}
		}
		switch len(match) {
		case 0:
			return nil, cmdutil.FlagErrorf("revisor %q no encontrado en %s", n, ws)
		case 1:
			out = append(out, api.Account{UUID: match[0].UUID})
		default:
			return nil, cmdutil.FlagErrorf("revisor %q es ambiguo (%d coincidencias); usá account_id o uuid", n, len(match))
		}
	}
	return out, nil
}

func newCmdComment(f *factory.Factory) *cobra.Command {
	var body, bodyFile, path string
	var line, fromLine, replyTo int
	var task bool
	cmd := &cobra.Command{
		Use:   "comment [<pr>]",
		Short: "Comenta un PR: general, inline (archivo/línea), respuesta, o como tarea bloqueante",
		Example: `  $ prismatic-rainbow pr comment 42 --body "LGTM"
  $ prismatic-rainbow pr comment 42 --path src/api.ts --line 57 --body "Falta validar null" --task
  $ prismatic-rainbow pr comment 42 --reply-to 2011336521 --body "Corregido en abc123"
  $ prismatic-rainbow pr comment 42 --body-file informe-qa.md`,
		Args: cmdutil.MaxArgs(1, "pr comment "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			text, err := readBody(f, body, bodyFile)
			if err != nil {
				return err
			}
			if strings.TrimSpace(text) == "" {
				return cmdutil.FlagErrorf("--body o --body-file es requerido")
			}
			if (line > 0 || fromLine > 0) && path == "" {
				return cmdutil.FlagErrorf("--line/--from-line requieren --path")
			}
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			c, err := client.CreatePullRequestComment(t.Workspace, t.Repo, t.ID, api.CreateCommentInput{
				Body: text, Path: path, Line: line, FromLine: fromLine, ParentID: replyTo,
			})
			if err != nil {
				return err
			}
			out := struct {
				CommentID int    `json:"comment_id"`
				URL       string `json:"url"`
				TaskID    int    `json:"task_id,omitempty"`
			}{CommentID: c.ID, URL: c.Links.HTML.Href}
			if task {
				tk, err := client.CreatePullRequestTask(t.Workspace, t.Repo, t.ID, text, c.ID)
				if err != nil {
					return fmt.Errorf("comentario %d creado, pero falló la tarea: %w", c.ID, err)
				}
				out.TaskID = tk.ID
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, out)
			}
			fmt.Fprintf(f.IO.Out, "%s Comentario %d creado %s\n", f.IO.Green("✓"), out.CommentID, out.URL)
			if out.TaskID > 0 {
				fmt.Fprintf(f.IO.Out, "%s Tarea %d creada\n", f.IO.Green("✓"), out.TaskID)
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&body, "body", "b", "", "Texto (Markdown)")
	fl.StringVar(&bodyFile, "body-file", "", `Leer el texto de un archivo ("-" = stdin)`)
	fl.StringVar(&path, "path", "", "Archivo para comentario inline")
	fl.IntVar(&line, "line", 0, "Línea en la versión nueva del archivo")
	fl.IntVar(&fromLine, "from-line", 0, "Línea en la versión vieja (para comentar una línea eliminada)")
	fl.IntVar(&replyTo, "reply-to", 0, "ID del comentario al que se responde")
	fl.BoolVar(&task, "task", false, "Crear además una tarea bloqueante vinculada al comentario")
	return cmd
}

// newCmdReview builds approve / request-changes, which share shape.
func newCmdReview(f *factory.Factory, use, short, doneMsg, undoneMsg string, call func(*api.Client, *cmdutil.PRTarget, bool) error) *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   use + " [<pr>]",
		Short: short,
		Args:  cmdutil.MaxArgs(1, "pr "+use+" "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			if err := call(client, t, !remove); err != nil {
				return err
			}
			msg := doneMsg
			if remove {
				msg = undoneMsg
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, map[string]any{"id": t.ID, "action": use, "removed": remove})
			}
			fmt.Fprintf(f.IO.Out, "%s %s PR #%d\n", f.IO.Green("✓"), msg, t.ID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "Deshacer")
	return cmd
}

func newCmdApprove(f *factory.Factory) *cobra.Command {
	return newCmdReview(f, "approve", "Aprueba un PR (--remove retira la aprobación)", "Aprobado", "Aprobación retirada en",
		func(c *api.Client, t *cmdutil.PRTarget, on bool) error {
			return c.ApprovePullRequest(t.Workspace, t.Repo, t.ID, on)
		})
}

func newCmdRequestChanges(f *factory.Factory) *cobra.Command {
	return newCmdReview(f, "request-changes", "Pide cambios en un PR (--remove lo retira)", "Cambios pedidos en", "Pedido de cambios retirado en",
		func(c *api.Client, t *cmdutil.PRTarget, on bool) error {
			return c.RequestChanges(t.Workspace, t.Repo, t.ID, on)
		})
}

func newCmdMerge(f *factory.Factory) *cobra.Command {
	var strategy, message string
	var closeSource bool
	cmd := &cobra.Command{
		Use:   "merge [<pr>]",
		Short: "Mergea un PR",
		Example: `  $ prismatic-rainbow pr merge 42
  $ prismatic-rainbow pr merge 42 --strategy squash --close-source-branch`,
		Args: cmdutil.MaxArgs(1, "pr merge "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			s := strings.ReplaceAll(strings.ToLower(strategy), "-", "_")
			if s != "" && s != api.MergeCommit && s != api.Squash && s != api.FastForward {
				return cmdutil.FlagErrorf("--strategy inválida: %q (merge_commit, squash, fast_forward)", strategy)
			}
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			p, err := client.MergePullRequest(t.Workspace, t.Repo, t.ID, api.MergeInput{MergeStrategy: s, CloseSourceBranch: closeSource, Message: message})
			if err != nil {
				return err
			}
			v := newPRView(p)
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			fmt.Fprintf(f.IO.Out, "%s PR #%d mergeado (%s)\n", f.IO.Green("✓"), v.ID, v.State)
			return nil
		},
	}
	cmd.Flags().StringVar(&strategy, "strategy", "", "merge_commit, squash o fast_forward (default: el del repo)")
	cmd.Flags().StringVar(&message, "message", "", "Mensaje del commit de merge")
	cmd.Flags().BoolVar(&closeSource, "close-source-branch", false, "Cerrar la rama origen")
	return cmd
}
