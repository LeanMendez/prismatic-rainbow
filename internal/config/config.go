// Package config persists credentials/settings in YAML and resolves the
// final values with flags > environment variables > config file precedence.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppName names the config directory and the binary.
const AppName = "prismatic-rainbow"

// Environment variables. BITBUCKET_WORKSPACE and BITBUCKET_REPO_SLUG match
// the variables Bitbucket Pipelines already exports, so the CLI works
// inside a pipeline without extra configuration.
const (
	EnvEmail          = "BITBUCKET_EMAIL"
	EnvToken          = "BITBUCKET_TOKEN"
	EnvWorkspace      = "BITBUCKET_WORKSPACE"
	EnvRepo           = "BITBUCKET_REPO_SLUG"
	EnvAPIURL         = "BITBUCKET_API_URL"
	EnvCustomPrefixes = "CLICKUP_CUSTOM_ID_PREFIXES"
)

// Config is the on-disk configuration.
type Config struct {
	// Email is the Atlassian account email. Empty means the token is sent
	// as a Bearer token (workspace/project/repository access tokens).
	Email            string `yaml:"email,omitempty"`
	Token            string `yaml:"token"`
	DefaultWorkspace string `yaml:"default_workspace,omitempty"`
	// ClickUpCustomIDPrefixes lists custom task ID prefixes (e.g. "ENG"
	// for ENG-123) recognized by `pr links` / `pr context`.
	ClickUpCustomIDPrefixes []string `yaml:"clickup_custom_id_prefixes,omitempty"`
}

// Path returns the config file location, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, AppName, "config.yaml"), nil
}

// Load reads the config file; a missing file yields an empty Config.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parseando %s: %w", path, err)
	}
	return &cfg, nil
}

// Save writes cfg with 0600 permissions.
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creando directorio de config: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("escribiendo config: %w", err)
	}
	return nil
}

// Overrides carries flag values; empty means "not set".
type Overrides struct {
	Email     string
	Token     string
	Workspace string
}

// Resolved is the effective configuration.
type Resolved struct {
	Email                   string
	Token                   string
	DefaultWorkspace        string
	ClickUpCustomIDPrefixes []string
	// Source describes where the token came from: flag, env or the config path.
	Source string
}

// Resolve applies flags > env > file precedence.
func Resolve(o Overrides) (*Resolved, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	path, _ := Path()
	r := &Resolved{
		Email:                   cfg.Email,
		Token:                   cfg.Token,
		DefaultWorkspace:        cfg.DefaultWorkspace,
		ClickUpCustomIDPrefixes: cfg.ClickUpCustomIDPrefixes,
		Source:                  path,
	}
	if v := os.Getenv(EnvToken); v != "" {
		// Env credentials replace the file pair as a unit so a stale file
		// email can't turn an env access token into a Basic-auth attempt.
		r.Token, r.Email, r.Source = v, os.Getenv(EnvEmail), "env "+EnvToken
	} else if v := os.Getenv(EnvEmail); v != "" {
		r.Email = v
	}
	if v := os.Getenv(EnvWorkspace); v != "" {
		r.DefaultWorkspace = v
	}
	if v := os.Getenv(EnvCustomPrefixes); v != "" {
		r.ClickUpCustomIDPrefixes = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				r.ClickUpCustomIDPrefixes = append(r.ClickUpCustomIDPrefixes, p)
			}
		}
	}
	if o.Token != "" {
		r.Token, r.Email, r.Source = o.Token, o.Email, "flag --token"
	} else if o.Email != "" {
		r.Email = o.Email
	}
	if o.Workspace != "" {
		r.DefaultWorkspace = o.Workspace
	}
	return r, nil
}

// ErrNoCredentials is returned when no token could be resolved.
var ErrNoCredentials = errors.New("no hay credenciales: ejecutá '" + AppName + " auth login' o seteá " + EnvToken + " (y " + EnvEmail + " si es un API token de usuario)")
