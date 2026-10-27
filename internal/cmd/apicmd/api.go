// Package apicmd implements `api`, a raw passthrough to the Bitbucket
// Cloud REST API for endpoints without a dedicated command.
package apicmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
)

// NewCmdAPI builds the `api` command.
func NewCmdAPI(f *factory.Factory) *cobra.Command {
	var method, input string
	var paginate bool
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Request crudo a la API 2.0 (escape hatch)",
		Long: `Hace un request autenticado a https://api.bitbucket.org/2.0<path>.

Los placeholders {workspace} y {repo} se reemplazan con el repo resuelto
(remote git, --repo, BITBUCKET_REPO_SLUG). Con --paginate sigue los links
"next" e imprime un único array JSON con todos los "values".

Métodos distintos de GET requieren -X explícito. El body se lee de --input
(archivo, o "-" para stdin).`,
		Example: `  $ prismatic-rainbow api /repositories/{workspace}/{repo}/pullrequests/42/activity
  $ prismatic-rainbow api /repositories/{workspace}/{repo}/pipelines/ --paginate
  $ echo '{"content":{"raw":"hola"}}' | prismatic-rainbow api -X POST /repositories/{workspace}/{repo}/pullrequests/42/comments --input -`,
		Args: cmdutil.ExactArgs(1, "api requiere el path del endpoint"),
		RunE: func(_ *cobra.Command, args []string) error {
			client, err := f.Client()
			if err != nil {
				return err
			}
			path := args[0]
			if strings.Contains(path, "{workspace}") || strings.Contains(path, "{repo}") {
				ws, repo, err := cmdutil.ResolveRepo(f)
				if err != nil {
					return err
				}
				path = strings.NewReplacer("{workspace}", ws, "{repo}", repo).Replace(path)
			}
			method = strings.ToUpper(method)
			if paginate && method != http.MethodGet {
				return cmdutil.FlagErrorf("--paginate solo aplica a GET")
			}

			var body []byte
			if input != "" {
				var r io.Reader = f.IO.In
				if input != "-" {
					file, err := os.Open(input)
					if err != nil {
						return err
					}
					defer file.Close()
					r = file
				}
				if body, err = io.ReadAll(r); err != nil {
					return err
				}
			}

			if paginate {
				var all []json.RawMessage
				next := path
				for next != "" {
					resp, err := client.Do(http.MethodGet, next, nil, "", "")
					if err != nil {
						return err
					}
					var p struct {
						Values []json.RawMessage `json:"values"`
						Next   string            `json:"next"`
					}
					if err := json.Unmarshal(resp.Body, &p); err != nil {
						return fmt.Errorf("la respuesta no es una página JSON de Bitbucket: %w", err)
					}
					all = append(all, p.Values...)
					next = p.Next
				}
				if all == nil {
					all = []json.RawMessage{}
				}
				return cmdutil.PrintJSON(f.IO.Out, all)
			}

			ct := ""
			if body != nil {
				ct = "application/json"
			}
			resp, err := client.Do(method, path, body, ct, "*/*")
			if resp != nil && len(resp.Body) > 0 {
				var pretty bytes.Buffer
				if json.Indent(&pretty, resp.Body, "", "  ") == nil {
					pretty.WriteByte('\n')
					_, _ = f.IO.Out.Write(pretty.Bytes())
				} else {
					_, _ = f.IO.Out.Write(resp.Body)
				}
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&method, "method", "X", "GET", "Método HTTP")
	cmd.Flags().StringVar(&input, "input", "", `Archivo con el body JSON ("-" = stdin)`)
	cmd.Flags().BoolVar(&paginate, "paginate", false, "Seguir paginación y devolver todos los values")
	return cmd
}
