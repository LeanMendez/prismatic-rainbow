// Package cmdutil holds helpers shared by command packages: usage errors,
// silent exit codes, repo/PR resolution and output helpers.
package cmdutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/config"
	"github.com/LeanMendez/prismatic-rainbow/internal/gitctx"
)

// FlagError is a usage error (exit code 2).
type FlagError struct{ Err error }

func (e *FlagError) Error() string { return e.Err.Error() }
func (e *FlagError) Unwrap() error { return e.Err }

// FlagErrorf builds a *FlagError.
func FlagErrorf(format string, a ...any) error {
	return &FlagError{Err: fmt.Errorf(format, a...)}
}

// ExitError ends the process with Code without printing anything (the
// command already wrote its output), e.g. `pr checks` with pending checks.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "exit " + strconv.Itoa(e.Code) }

// MaxArgs validates at most n positional args as a usage error.
func MaxArgs(n int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > n {
			return FlagErrorf("%s", usage)
		}
		return nil
	}
}

// ExactArgs validates exactly n positional args as a usage error.
func ExactArgs(n int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return FlagErrorf("%s", usage)
		}
		return nil
	}
}

// ResolveWorkspace picks the workspace: --workspace > git remote >
// BITBUCKET_WORKSPACE / default_workspace.
func ResolveWorkspace(f *factory.Factory) (string, error) {
	if f.Options.Workspace != "" {
		return f.Options.Workspace, nil
	}
	if gc, err := f.GitContext(); err == nil {
		return gc.Workspace, nil
	}
	if cfg, err := f.Config(); err == nil && cfg.DefaultWorkspace != "" {
		return cfg.DefaultWorkspace, nil
	}
	return "", FlagErrorf("no se pudo determinar el workspace: usá --workspace, %s o 'auth login'", config.EnvWorkspace)
}

// ResolveRepo picks workspace and repo. An explicit --repo uses
// --workspace or the configured default workspace; otherwise the git
// remote wins (workspace and repo come together so they never mismatch),
// then BITBUCKET_REPO_SLUG (as exported by Bitbucket Pipelines).
func ResolveRepo(f *factory.Factory) (ws, repo string, err error) {
	defaultWS := func() string {
		if f.Options.Workspace != "" {
			return f.Options.Workspace
		}
		if cfg, err := f.Config(); err == nil {
			return cfg.DefaultWorkspace
		}
		return ""
	}
	switch {
	case f.Options.Repo != "":
		ws, repo = defaultWS(), f.Options.Repo
		if w, r, ok := strings.Cut(repo, "/"); ok {
			ws, repo = w, r
		}
	default:
		if gc, gerr := f.GitContext(); gerr == nil {
			ws, repo = gc.Workspace, gc.Repo
			if f.Options.Workspace != "" {
				ws = f.Options.Workspace
			}
		} else if env := os.Getenv(config.EnvRepo); env != "" {
			ws, repo = defaultWS(), env
		}
	}
	if ws == "" || repo == "" {
		return "", "", &FlagError{Err: gitctx.ErrNotBitbucketRepo}
	}
	return ws, repo, nil
}

// PRTarget identifies a pull request.
type PRTarget struct {
	Workspace string
	Repo      string
	ID        int
}

// ResolvePR accepts "42", "#42", a PR URL
// (https://bitbucket.org/ws/repo/pull-requests/42[/...]) or nothing, in
// which case the open PR whose source is the current git branch is used.
func ResolvePR(f *factory.Factory, client *api.Client, arg string) (*PRTarget, error) {
	if strings.Contains(arg, "://") {
		return parsePRURL(arg)
	}
	ws, repo, err := ResolveRepo(f)
	if err != nil {
		return nil, err
	}
	if arg != "" {
		id, err := strconv.Atoi(strings.TrimPrefix(arg, "#"))
		if err != nil || id <= 0 {
			return nil, FlagErrorf("ID de pull request inválido: %q (usá un número o la URL del PR)", arg)
		}
		return &PRTarget{ws, repo, id}, nil
	}
	gc, err := f.GitContext()
	if err != nil || gc.Branch == "" {
		return nil, FlagErrorf("pasá el ID del pull request (no se pudo inferir desde la rama git actual)")
	}
	prs, err := client.FindOpenPullRequestForBranch(ws, repo, gc.Branch)
	if err != nil {
		return nil, err
	}
	switch len(prs) {
	case 0:
		return nil, FlagErrorf("no hay un PR abierto con rama origen %q; pasá el ID", gc.Branch)
	case 1:
		return &PRTarget{ws, repo, prs[0].ID}, nil
	default:
		ids := make([]string, len(prs))
		for i, p := range prs {
			ids[i] = "#" + strconv.Itoa(p.ID)
		}
		return nil, FlagErrorf("hay varios PRs abiertos desde %q (%s); pasá el ID", gc.Branch, strings.Join(ids, ", "))
	}
}

func parsePRURL(raw string) (*PRTarget, error) {
	u, err := url.Parse(raw)
	if err == nil && strings.EqualFold(u.Hostname(), "bitbucket.org") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull-requests" {
			if id, err := strconv.Atoi(parts[3]); err == nil && id > 0 {
				return &PRTarget{parts[0], parts[1], id}, nil
			}
		}
	}
	return nil, FlagErrorf("URL de PR no reconocida: %q (esperado https://bitbucket.org/<ws>/<repo>/pull-requests/<id>)", raw)
}

// PrintJSON writes v as indented JSON.
func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// JSONError is the error envelope printed to stderr with --output json.
type JSONError struct {
	Error struct {
		Code           string   `json:"code"`
		Message        string   `json:"message"`
		HTTPStatus     int      `json:"http_status,omitempty"`
		RequiredScopes []string `json:"required_scopes,omitempty"`
		Retryable      bool     `json:"retryable,omitempty"`
	} `json:"error"`
}

// NewJSONError classifies err for machine consumers.
func NewJSONError(err error) JSONError {
	var out JSONError
	out.Error.Message = err.Error()
	var apiErr *api.Error
	var flagErr *FlagError
	switch {
	case errors.As(err, &apiErr):
		out.Error.Code = apiErr.Code()
		out.Error.HTTPStatus = apiErr.StatusCode
		out.Error.RequiredScopes = apiErr.RequiredScopes
		out.Error.Retryable = apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
	case errors.Is(err, config.ErrNoCredentials):
		out.Error.Code = "no_credentials"
	case errors.As(err, &flagErr):
		out.Error.Code = "usage"
	default:
		out.Error.Code = "error"
	}
	return out
}

// Truncate shortens s to max runes with an ellipsis.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// FirstLine returns the first line of s.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// Ago formats t relative to now ("3d", "5h").
func Ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "ahora"
	case d < time.Hour:
		return fmt.Sprintf("hace %dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("hace %dh", int(d.Hours()))
	default:
		return fmt.Sprintf("hace %dd", int(d.Hours()/24))
	}
}

// ShortHash returns the first 12 characters of a commit hash.
func ShortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
