// Package src reads repository files and directories at any ref without
// cloning: `src cat` and `src ls`. FetchFile is reused by `pr file`.
package src

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

// FileView is the JSON shape of a file read.
type FileView struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref"`
	Commit     string `json:"commit"`
	Path       string `json:"path"`
	Binary     bool   `json:"binary"`
	TotalLines int    `json:"total_lines"`
	// StartLine/EndLine are set when --lines selected a range (1-based, inclusive).
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Content   string `json:"content"`
}

// LineRange is a parsed --lines value; zero value = whole file.
type LineRange struct{ Start, End int }

// ParseLineRange parses "10:40", "10:" or ":40".
func ParseLineRange(s string) (LineRange, error) {
	if s == "" {
		return LineRange{}, nil
	}
	a, b, ok := strings.Cut(s, ":")
	if !ok {
		return LineRange{}, cmdutil.FlagErrorf("--lines inválido: %q (formato inicio:fin, p.ej. 10:40)", s)
	}
	var r LineRange
	var err error
	if a != "" {
		if r.Start, err = strconv.Atoi(a); err != nil || r.Start < 1 {
			return LineRange{}, cmdutil.FlagErrorf("--lines inválido: %q", s)
		}
	}
	if b != "" {
		if r.End, err = strconv.Atoi(b); err != nil || r.End < 1 || (r.Start > 0 && r.End < r.Start) {
			return LineRange{}, cmdutil.FlagErrorf("--lines inválido: %q", s)
		}
	}
	return r, nil
}

// FetchFile reads path at commit and applies the line range.
func FetchFile(client *api.Client, ws, repo, ref, commit, path string, lr LineRange) (*FileView, error) {
	data, err := client.GetFileContent(ws, repo, commit, path)
	if err != nil {
		return nil, err
	}
	v := &FileView{Repository: ws + "/" + repo, Ref: ref, Commit: commit, Path: strings.Trim(path, "/")}
	probe := data
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		v.Binary = true
		return v, nil
	}
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	v.TotalLines = len(lines)
	if lr == (LineRange{}) {
		v.Content = text
		return v, nil
	}
	start, end := max(lr.Start, 1), lr.End
	if end == 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return nil, cmdutil.FlagErrorf("--lines fuera de rango: el archivo tiene %d líneas", len(lines))
	}
	v.StartLine, v.EndLine = start, end
	v.Content = strings.Join(lines[start-1:end], "")
	return v, nil
}

// NewCmdSrc groups src subcommands.
func NewCmdSrc(f *factory.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "src",
		Short: "Lee archivos y directorios del repo en cualquier rama/tag/commit, sin clonar",
	}
	cmd.AddCommand(newCmdCat(f), newCmdLs(f))
	return cmd
}

// resolveRef returns the ref to show and its commit hash; "" ref = main branch.
func resolveRef(client *api.Client, ws, repo, ref string) (string, string, error) {
	if ref == "" {
		r, err := client.GetRepository(ws, repo)
		if err != nil {
			return "", "", err
		}
		ref = r.MainBranch()
	}
	commit, err := client.ResolveRef(ws, repo, ref)
	return ref, commit, err
}

func newCmdCat(f *factory.Factory) *cobra.Command {
	var ref, lines string
	cmd := &cobra.Command{
		Use:   "cat <path>",
		Short: "Imprime un archivo",
		Example: `  $ prismatic-rainbow src cat package.json
  $ prismatic-rainbow src cat src/app.ts --ref feature/CU-86b1x2y3z_login --lines 1:80
  $ prismatic-rainbow src cat README.md --ref v1.2.0 -o json`,
		Args: cmdutil.ExactArgs(1, "src cat requiere la ruta del archivo"),
		RunE: func(_ *cobra.Command, args []string) error {
			lr, err := ParseLineRange(lines)
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
			ref, commit, err := resolveRef(client, ws, repo, ref)
			if err != nil {
				return err
			}
			v, err := FetchFile(client, ws, repo, ref, commit, args[0], lr)
			if err != nil {
				return err
			}
			return PrintFile(f, v)
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Rama, tag o commit (por defecto la rama principal)")
	cmd.Flags().StringVar(&lines, "lines", "", "Rango de líneas inicio:fin (1-based, inclusivo)")
	return cmd
}

// PrintFile renders a FileView as JSON or raw text.
func PrintFile(f *factory.Factory, v *FileView) error {
	if f.JSON() {
		return cmdutil.PrintJSON(f.IO.Out, v)
	}
	if v.Binary {
		return fmt.Errorf("%s es un archivo binario; no se imprime", v.Path)
	}
	_, err := fmt.Fprint(f.IO.Out, v.Content)
	return err
}

func newCmdLs(f *factory.Factory) *cobra.Command {
	var ref string
	var depth, limit int
	cmd := &cobra.Command{
		Use:   "ls [path]",
		Short: "Lista un directorio",
		Example: `  $ prismatic-rainbow src ls
  $ prismatic-rainbow src ls src/modules --ref develop --depth 3 -o json`,
		Args: cmdutil.MaxArgs(1, "src ls acepta como máximo una ruta"),
		RunE: func(_ *cobra.Command, args []string) error {
			client, err := f.Client()
			if err != nil {
				return err
			}
			ws, repo, err := cmdutil.ResolveRepo(f)
			if err != nil {
				return err
			}
			ref, commit, err := resolveRef(client, ws, repo, ref)
			if err != nil {
				return err
			}
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			entries, err := client.ListDirectory(ws, repo, commit, path, depth, limit)
			if err != nil {
				return err
			}
			type entry struct {
				Path string `json:"path"`
				Type string `json:"type"` // file | dir
				Size int64  `json:"size,omitempty"`
			}
			out := struct {
				Ref     string  `json:"ref"`
				Commit  string  `json:"commit"`
				Entries []entry `json:"entries"`
			}{Ref: ref, Commit: commit, Entries: make([]entry, len(entries))}
			for i, e := range entries {
				typ := "file"
				if e.Type == "commit_directory" {
					typ = "dir"
				}
				out.Entries[i] = entry{e.Path, typ, e.Size}
			}
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, out)
			}
			t := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
			for _, e := range out.Entries {
				name := e.Path
				if e.Type == "dir" {
					name += "/"
				}
				t.AddRow(name)
			}
			return t.Render()
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Rama, tag o commit (por defecto la rama principal)")
	cmd.Flags().IntVar(&depth, "depth", 1, "Profundidad de recursión")
	cmd.Flags().IntVar(&limit, "limit", 0, "Máximo de entradas (0 = todas)")
	return cmd
}
