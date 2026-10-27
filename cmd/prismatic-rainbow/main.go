// Command prismatic-rainbow is a Bitbucket Cloud CLI for humans and agents.
// This file only maps errors to exit codes:
//
//	0 ok · 1 error · 2 usage · 4 auth/permissions · 8 checks pending (pr checks)
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/root"
	"github.com/LeanMendez/prismatic-rainbow/internal/config"
)

// version is set with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	opts := &factory.GlobalOptions{}
	f := factory.New(version, opts)
	err := root.NewCmdRoot(f).Execute()
	if err == nil {
		return 0
	}
	var exitErr *cmdutil.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	if f.JSON() {
		_ = cmdutil.PrintJSON(os.Stderr, cmdutil.NewJSONError(err))
	} else {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	var flagErr *cmdutil.FlagError
	var apiErr *api.Error
	switch {
	case errors.As(err, &flagErr):
		return 2
	case errors.Is(err, config.ErrNoCredentials):
		return 4
	case errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403):
		return 4
	}
	return 1
}
