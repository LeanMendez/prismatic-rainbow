// Package auth implements `auth login` and `auth status`.
package auth

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/config"
)

// NewCmdAuth groups the auth subcommands.
func NewCmdAuth(f *factory.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Credenciales de Bitbucket Cloud"}
	cmd.AddCommand(newCmdLogin(f), newCmdStatus(f))
	return cmd
}

// Identity is what we learned while validating credentials.
type Identity struct {
	Authenticated bool   `json:"authenticated"`
	AuthMode      string `json:"auth_mode"` // basic | bearer
	User          string `json:"user,omitempty"`
	AccountID     string `json:"account_id,omitempty"`
	// Repository is set when a Bearer access token (no user identity) was
	// validated by reading a repository instead of /user.
	Repository       string `json:"repository,omitempty"`
	Source           string `json:"source,omitempty"`
	DefaultWorkspace string `json:"default_workspace,omitempty"`
}

// validate checks the credentials. Access tokens have no user, so for
// Bearer tokens a failing /user falls back to reading the current repo.
func validate(f *factory.Factory, client *api.Client) (*Identity, error) {
	id := &Identity{AuthMode: client.AuthMode()}
	user, err := client.CurrentUser()
	if err == nil {
		id.Authenticated, id.User, id.AccountID = true, user.DisplayName, user.AccountID
		return id, nil
	}
	if client.AuthMode() != "bearer" {
		return nil, err
	}
	ws, repo, rerr := cmdutil.ResolveRepo(f)
	if rerr != nil {
		return nil, fmt.Errorf("%w (si es un access token de workspace/repo, ejecutá dentro del repo o pasá --repo para validarlo)", err)
	}
	if _, rerr := client.GetRepository(ws, repo); rerr != nil {
		return nil, rerr
	}
	id.Authenticated, id.Repository = true, ws+"/"+repo
	return id, nil
}

func newCmdLogin(f *factory.Factory) *cobra.Command {
	var withToken bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Guarda y valida credenciales",
		Long: `Guarda credenciales en ~/.config/prismatic-rainbow/config.yaml (0600).

Dos modos:
  - API token de usuario (Atlassian, "API token with scopes", app Bitbucket):
    se usa con tu email por HTTP Basic.
  - Access token de workspace/proyecto/repo: sin email, se envía como Bearer.
    Recomendado para agentes/bots (rate limits más altos y sin identidad personal).

Sin flags pregunta de forma interactiva. Con --with-token lee el token de stdin.`,
		Example: `  $ prismatic-rainbow auth login
  $ prismatic-rainbow auth login --email vos@empresa.com --token ATATT...
  $ echo "$TOKEN" | prismatic-rainbow auth login --with-token -R empresa/proyecto`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return loginRun(f, f.Options.Email, f.Options.Token, withToken)
		},
	}
	cmd.Flags().BoolVar(&withToken, "with-token", false, "Leer el token desde stdin")
	return cmd
}

func loginRun(f *factory.Factory, email, token string, withToken bool) error {
	io := f.IO
	interactive := io.IsStdinTTY() && !withToken && token == ""
	if withToken {
		line, err := io.ReadLine()
		if err != nil {
			return fmt.Errorf("leyendo token de stdin: %w", err)
		}
		token = line
	}
	if interactive {
		if email == "" {
			fmt.Fprint(io.ErrOut, "Email de Atlassian (Enter si es un access token): ")
			line, err := io.ReadLine()
			if err != nil {
				return err
			}
			email = line
		}
		fmt.Fprint(io.ErrOut, "Token: ")
		t, err := io.ReadSecret()
		fmt.Fprintln(io.ErrOut)
		if err != nil {
			return err
		}
		token = t
	}
	email, token = strings.TrimSpace(email), strings.TrimSpace(token)
	if token == "" {
		return cmdutil.FlagErrorf("falta el token: usá --token, --with-token o ejecutá en una terminal interactiva")
	}

	client := f.NewClient(email, token)
	id, err := validate(f, client)
	if err != nil {
		return fmt.Errorf("no se pudieron validar las credenciales: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Email, cfg.Token = email, token
	if interactive && id.User != "" {
		if ws := pickWorkspace(f, client); ws != "" {
			cfg.DefaultWorkspace = ws
		}
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	id.DefaultWorkspace = cfg.DefaultWorkspace
	id.Source, _ = config.Path()
	if f.JSON() {
		return cmdutil.PrintJSON(io.Out, id)
	}
	who := id.User
	if who == "" {
		who = "access token de " + id.Repository
	}
	fmt.Fprintf(io.Out, "%s Autenticado como %s (%s)\n", io.Green("✓"), who, id.AuthMode)
	return nil
}

func pickWorkspace(f *factory.Factory, client *api.Client) string {
	io := f.IO
	wss, err := client.ListWorkspaces(0)
	if err != nil || len(wss) == 0 {
		return ""
	}
	fmt.Fprintln(io.ErrOut, "Workspaces:")
	for i, w := range wss {
		fmt.Fprintf(io.ErrOut, "  %d. %s\n", i+1, w.Slug)
	}
	fmt.Fprint(io.ErrOut, "Workspace por defecto (número, Enter para omitir): ")
	line, _ := io.ReadLine()
	if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= len(wss) {
		return wss[n-1].Slug
	}
	return ""
}

func newCmdStatus(f *factory.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Valida las credenciales configuradas contra la API",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			client, err := f.Client()
			if err != nil {
				return err
			}
			id, err := validate(f, client)
			if err != nil {
				return err
			}
			id.Source, id.DefaultWorkspace = cfg.Source, cfg.DefaultWorkspace
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, id)
			}
			io := f.IO
			if id.User != "" {
				fmt.Fprintf(io.Out, "%s Autenticado como %s\n", io.Green("✓"), id.User)
			} else {
				fmt.Fprintf(io.Out, "%s Access token válido para %s\n", io.Green("✓"), id.Repository)
			}
			fmt.Fprintf(io.Out, "Modo: %s · Credenciales: %s\n", id.AuthMode, id.Source)
			if id.DefaultWorkspace != "" {
				fmt.Fprintf(io.Out, "Workspace por defecto: %s\n", id.DefaultWorkspace)
			}
			return nil
		},
	}
}
