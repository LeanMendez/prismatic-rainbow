package pr

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/src"
	"github.com/LeanMendez/prismatic-rainbow/internal/diffparse"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

func newCmdFiles(f *factory.Factory) *cobra.Command {
	var include, exclude []string
	cmd := &cobra.Command{
		Use:   "files [<pr>]",
		Short: "Archivos cambiados con status y líneas +/- (diffstat, sin patch)",
		Example: `  $ prismatic-rainbow pr files 42
  $ prismatic-rainbow pr files 42 --exclude '*.lock' -o json`,
		Args: cmdutil.MaxArgs(1, "pr files "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			stats, err := client.GetPullRequestDiffStat(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			v := newFilesView(stats)
			v = filterFiles(v, include, exclude)
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			tp := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			tp.AddHeader("STATUS", "+", "-", "ARCHIVO")
			for _, fs := range v.Files {
				name := fs.Path
				if fs.OldPath != "" {
					name = fs.OldPath + " → " + fs.Path
				}
				tp.AddRow(fs.Status, itoa(fs.LinesAdded), itoa(fs.LinesRemoved), name)
			}
			if err := tp.Render(); err != nil {
				return err
			}
			if f.IO.IsStdoutTTY() {
				fmt.Fprintf(f.IO.Out, "\n%d archivos · +%d -%d\n", v.Count, v.LinesAdded, v.LinesRemoved)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&include, "path", nil, "Solo archivos que coincidan (ruta, directorio o glob; repetible)")
	cmd.Flags().StringArrayVar(&exclude, "exclude", nil, "Excluir archivos que coincidan (repetible)")
	return cmd
}

func filterFiles(v FilesView, include, exclude []string) FilesView {
	if len(include) == 0 && len(exclude) == 0 {
		return v
	}
	parsed := make([]diffparse.File, len(v.Files))
	for i, fs := range v.Files {
		parsed[i] = diffparse.File{Path: fs.Path, OldPath: fs.OldPath}
	}
	keep := map[string]bool{}
	for _, p := range diffparse.Filter(parsed, include, exclude) {
		keep[p.Path] = true
	}
	out := FilesView{Files: []FileStatView{}}
	for _, fs := range v.Files {
		if keep[fs.Path] {
			out.Files = append(out.Files, fs)
			out.LinesAdded += fs.LinesAdded
			out.LinesRemoved += fs.LinesRemoved
		}
	}
	out.Count = len(out.Files)
	return out
}

func newCmdFile(f *factory.Factory) *cobra.Command {
	var side, lines string
	cmd := &cobra.Command{
		Use:   "file [<pr>] <path>",
		Short: "Contenido completo de un archivo en el commit de origen (o destino) del PR",
		Long: `Lee un archivo tal como queda en la rama del PR (--side source, default) o
como está en la rama destino (--side destination), sin clonar ni hacer checkout.
Sirve para que un agente lea el archivo entero y no solo los hunks del diff.
Para PRs desde forks lee del repo de origen.`,
		Example: `  $ prismatic-rainbow pr file 42 src/auth/login.ts
  $ prismatic-rainbow pr file 42 src/auth/login.ts --side destination
  $ prismatic-rainbow pr file 42 src/auth/login.ts --lines 120:180 -o json`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return cmdutil.FlagErrorf("pr file requiere [<pr>] <path>")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			lr, err := src.ParseLineRange(lines)
			if err != nil {
				return err
			}
			if side != "source" && side != "destination" {
				return cmdutil.FlagErrorf("--side inválido: %q (source o destination)", side)
			}
			path := args[len(args)-1]
			client, t, err := resolve(f, args[:len(args)-1])
			if err != nil {
				return err
			}
			p, err := client.GetPullRequest(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			end := p.Source
			if side == "destination" {
				end = p.Destination
			}
			ws, repo := t.Workspace, t.Repo
			if full := end.RepoFullName(); full != "" {
				if w, r, ok := strings.Cut(full, "/"); ok {
					ws, repo = w, r
				}
			}
			commit := end.CommitHash()
			if commit == "" {
				return fmt.Errorf("el PR no informa el commit de %s", side)
			}
			v, err := src.FetchFile(client, ws, repo, end.Branch.Name, commit, path, lr)
			if err != nil {
				return err
			}
			return src.PrintFile(f, v)
		},
	}
	cmd.Flags().StringVar(&side, "side", "source", "source (rama del PR) o destination")
	cmd.Flags().StringVar(&lines, "lines", "", "Rango de líneas inicio:fin (1-based, inclusivo)")
	return cmd
}
