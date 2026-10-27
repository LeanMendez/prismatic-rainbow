package pr

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/diffparse"
)

// DiffFlags controls which part of the diff is returned.
type DiffFlags struct {
	Include          []string
	Exclude          []string
	Context          int
	IgnoreWhitespace bool
	MaxLinesPerFile  int
	MaxBytes         int
}

func (d *DiffFlags) register(cmd *cobra.Command, defaultMaxBytes int) {
	fl := cmd.Flags()
	fl.StringArrayVar(&d.Include, "path", nil, "Solo archivos que coincidan (ruta, directorio o glob; repetible)")
	fl.StringArrayVar(&d.Exclude, "exclude", nil, "Excluir archivos que coincidan (p.ej. '*.lock', 'dist/'; repetible)")
	fl.IntVar(&d.Context, "context", -1, "Líneas de contexto por hunk (-1 = default de Bitbucket)")
	fl.BoolVar(&d.IgnoreWhitespace, "ignore-whitespace", false, "Ignorar cambios de espacios")
	fl.IntVar(&d.MaxLinesPerFile, "max-lines-per-file", 0, "Recortar cada archivo a N líneas de patch (60% inicio / 40% final; 0 = sin límite)")
	fl.IntVar(&d.MaxBytes, "max-bytes", defaultMaxBytes, "Tope total de bytes de patch; los archivos que no entran se listan en omitted_files (0 = sin límite)")
}

// DiffView is the JSON shape of a (possibly filtered/truncated) diff.
type DiffView struct {
	// TotalFiles counts files in the full PR diff, before --path/--exclude.
	TotalFiles   int              `json:"total_files"`
	Files        []diffparse.File `json:"files"`
	OmittedFiles []string         `json:"omitted_files,omitempty"`
	Truncated    bool             `json:"truncated"`
}

func fetchDiff(client *api.Client, t *cmdutil.PRTarget, d DiffFlags) (*DiffView, error) {
	raw, err := client.GetPullRequestDiff(t.Workspace, t.Repo, t.ID, api.DiffOptions{Context: d.Context, IgnoreWhitespace: d.IgnoreWhitespace})
	if err != nil {
		return nil, err
	}
	all := diffparse.Parse(raw)
	files := diffparse.Filter(all, d.Include, d.Exclude)
	v := &DiffView{TotalFiles: len(all), Files: files}
	for i := range files {
		diffparse.LimitLines(&files[i], d.MaxLinesPerFile)
		v.Truncated = v.Truncated || files[i].Truncated
	}
	v.OmittedFiles = diffparse.LimitBytes(files, d.MaxBytes)
	v.Truncated = v.Truncated || len(v.OmittedFiles) > 0
	return v, nil
}

func newCmdDiff(f *factory.Factory) *cobra.Command {
	var d DiffFlags
	var nameOnly bool
	cmd := &cobra.Command{
		Use:   "diff [<pr>]",
		Short: "Diff unificado del PR, filtrable y acotable",
		Long: `Muestra el diff del PR (rama origen vs. merge-base con destino, como en la UI).

--path y --exclude aceptan rutas exactas, directorios ("src/api" o "src/api/")
y globs (*, ?, **). Un glob sin "/" también se compara contra el nombre del
archivo, así '*.lock' excluye 'web/yarn.lock'.

Con -o json devuelve un archivo por entrada con status, líneas +/- y patch, y
marca truncated/omitted_files cuando se aplicaron límites.`,
		Example: `  $ prismatic-rainbow pr diff 42
  $ prismatic-rainbow pr diff 42 --name-only
  $ prismatic-rainbow pr diff 42 --path src/auth --exclude '*.snap' -o json
  $ prismatic-rainbow pr diff 42 --max-lines-per-file 200 --max-bytes 100000`,
		Args: cmdutil.MaxArgs(1, "pr diff "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			v, err := fetchDiff(client, t, d)
			if err != nil {
				return err
			}
			out := f.IO.Out
			if nameOnly {
				if f.JSON() {
					names := make([]string, len(v.Files))
					for i, file := range v.Files {
						names[i] = file.Path
					}
					return cmdutil.PrintJSON(out, names)
				}
				for _, file := range v.Files {
					fmt.Fprintln(out, file.Path)
				}
				return nil
			}
			if f.JSON() {
				return cmdutil.PrintJSON(out, v)
			}
			_, err = fmt.Fprint(out, diffparse.Join(v.Files))
			if len(v.OmittedFiles) > 0 {
				fmt.Fprintf(f.IO.ErrOut, "aviso: %d archivo(s) sin patch por --max-bytes: usá --path para pedirlos\n", len(v.OmittedFiles))
			}
			return err
		},
	}
	d.register(cmd, 0)
	cmd.Flags().BoolVar(&nameOnly, "name-only", false, "Solo listar las rutas cambiadas")
	return cmd
}
