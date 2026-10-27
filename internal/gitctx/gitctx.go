// Package gitctx infers the Bitbucket workspace, repository and current
// branch from the local git checkout.
package gitctx

import (
	"errors"
	"net/url"
	"os/exec"
	"strings"
)

// ErrNotBitbucketRepo means no git remote points to Bitbucket Cloud.
var ErrNotBitbucketRepo = errors.New("no se pudo inferir el repo de Bitbucket: ejecutá dentro de un clon con un remote a bitbucket.org, o pasá --workspace y --repo")

// Context is what we could infer from git.
type Context struct {
	Workspace string
	Repo      string
	Branch    string
	Remote    string
}

// Current inspects the git repository in the working directory. It
// prefers the "origin" remote and falls back to the first Bitbucket remote.
func Current() (*Context, error) {
	out, err := exec.Command("git", "remote").Output()
	if err != nil {
		return nil, ErrNotBitbucketRepo
	}
	remotes := strings.Fields(string(out))
	ordered := make([]string, 0, len(remotes))
	for _, r := range remotes {
		if r == "origin" {
			ordered = append([]string{r}, ordered...)
		} else {
			ordered = append(ordered, r)
		}
	}
	for _, name := range ordered {
		u, err := exec.Command("git", "remote", "get-url", name).Output()
		if err != nil {
			continue
		}
		ws, repo, err := ParseRemoteURL(string(u))
		if err != nil {
			continue
		}
		ctx := &Context{Workspace: ws, Repo: repo, Remote: name}
		// symbolic-ref also works on an unborn branch and fails when detached.
		if b, err := exec.Command("git", "symbolic-ref", "--quiet", "--short", "HEAD").Output(); err == nil {
			ctx.Branch = strings.TrimSpace(string(b))
		}
		return ctx, nil
	}
	return nil, ErrNotBitbucketRepo
}

// ParseRemoteURL extracts workspace/repo from SSH shorthand
// (git@bitbucket.org:ws/repo.git), ssh:// and http(s):// remotes. Both
// bitbucket.org and ssh.bitbucket.org hosts are accepted.
func ParseRemoteURL(raw string) (workspace, repo string, err error) {
	raw = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(raw), "/"), ".git")
	var host, path string
	switch {
	case strings.Contains(raw, "://"):
		u, perr := url.Parse(raw)
		if perr != nil {
			return "", "", ErrNotBitbucketRepo
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(raw, "@") && strings.Contains(raw, ":"):
		_, rest, _ := strings.Cut(raw, "@")
		host, path, _ = strings.Cut(rest, ":")
	default:
		return "", "", ErrNotBitbucketRepo
	}
	if !isBitbucketHost(host) {
		return "", "", ErrNotBitbucketRepo
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrNotBitbucketRepo
	}
	return parts[0], parts[1], nil
}

func isBitbucketHost(h string) bool {
	h = strings.ToLower(h)
	return h == "bitbucket.org" || h == "ssh.bitbucket.org" || h == "altssh.bitbucket.org"
}
