// Package factory builds the dependencies shared by every command. Fields
// are funcs so tests can substitute fakes without touching real config,
// network or git.
package factory

import (
	"os"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/config"
	"github.com/LeanMendez/prismatic-rainbow/internal/gitctx"
	"github.com/LeanMendez/prismatic-rainbow/internal/iostreams"
)

// GlobalOptions holds the persistent flag values (bound by root).
type GlobalOptions struct {
	Email     string
	Token     string
	Workspace string
	Repo      string
	Output    string
}

// Factory wires the dependencies.
type Factory struct {
	IO         *iostreams.IOStreams
	Config     func() (*config.Resolved, error)
	Client     func() (*api.Client, error)
	GitContext func() (*gitctx.Context, error)
	// NewClient builds an unvalidated client from explicit credentials
	// (used by `auth login` before anything is saved).
	NewClient func(email, token string) *api.Client
	Options   *GlobalOptions
	Version   string
}

// JSON reports whether --output json was requested.
func (f *Factory) JSON() bool { return f.Options.Output == "json" }

// New builds the real Factory.
func New(version string, opts *GlobalOptions) *Factory {
	cfgFn := func() (*config.Resolved, error) {
		return config.Resolve(config.Overrides{Email: opts.Email, Token: opts.Token, Workspace: opts.Workspace})
	}
	newClient := func(email, token string) *api.Client {
		return api.NewClient(os.Getenv(config.EnvAPIURL), email, token, version)
	}
	return &Factory{
		IO:     iostreams.System(),
		Config: cfgFn,
		Client: func() (*api.Client, error) {
			r, err := cfgFn()
			if err != nil {
				return nil, err
			}
			if r.Token == "" {
				return nil, config.ErrNoCredentials
			}
			return newClient(r.Email, r.Token), nil
		},
		GitContext: gitctx.Current,
		NewClient:  newClient,
		Options:    opts,
		Version:    version,
	}
}
