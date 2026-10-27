package cmdutil

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/config"
	"github.com/LeanMendez/prismatic-rainbow/internal/gitctx"
)

func testFactory(opts factory.GlobalOptions, gc *gitctx.Context, defaultWS string) *factory.Factory {
	return &factory.Factory{
		Options: &opts,
		Config:  func() (*config.Resolved, error) { return &config.Resolved{DefaultWorkspace: defaultWS}, nil },
		GitContext: func() (*gitctx.Context, error) {
			if gc == nil {
				return nil, gitctx.ErrNotBitbucketRepo
			}
			return gc, nil
		},
	}
}

func TestResolveRepoPrecedence(t *testing.T) {
	git := &gitctx.Context{Workspace: "gitws", Repo: "gitrepo", Branch: "feat"}
	for _, tc := range []struct {
		name      string
		opts      factory.GlobalOptions
		gc        *gitctx.Context
		env       string
		wantWS    string
		wantRepo  string
		wantError bool
	}{
		{"git remote", factory.GlobalOptions{}, git, "", "gitws", "gitrepo", false},
		{"--repo ws/repo beats git", factory.GlobalOptions{Repo: "a/b"}, git, "", "a", "b", false},
		// An explicit --repo never borrows the workspace from an unrelated git remote.
		{"--repo slug uses default workspace", factory.GlobalOptions{Repo: "b"}, git, "", "defws", "b", false},
		{"pipelines env", factory.GlobalOptions{}, nil, "envrepo", "defws", "envrepo", false},
		{"nothing", factory.GlobalOptions{}, nil, "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(config.EnvRepo, tc.env)
			ws, repo, err := ResolveRepo(testFactory(tc.opts, tc.gc, "defws"))
			if tc.wantError != (err != nil) || ws != tc.wantWS || repo != tc.wantRepo {
				t.Errorf("got %q %q %v", ws, repo, err)
			}
		})
	}
}

func TestResolvePR(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		w.Write([]byte(`{"values":[{"id":77}]}`))
	}))
	defer srv.Close()
	client := api.NewClient(srv.URL, "", "t", "test")
	f := testFactory(factory.GlobalOptions{}, &gitctx.Context{Workspace: "acme", Repo: "api", Branch: "feature/CU-1"}, "")

	for arg, want := range map[string]PRTarget{
		"42":  {"acme", "api", 42},
		"#42": {"acme", "api", 42},
		"https://bitbucket.org/other/web/pull-requests/9/diff": {"other", "web", 9},
		"": {"acme", "api", 77},
	} {
		got, err := ResolvePR(f, client, arg)
		if err != nil || *got != want {
			t.Errorf("ResolvePR(%q) = %+v, %v; want %+v", arg, got, err, want)
		}
	}
	if gotQ != `source.branch.name="feature/CU-1"` {
		t.Errorf("branch lookup q = %s", gotQ)
	}
	if _, err := ResolvePR(f, client, "abc"); err == nil {
		t.Error("non-numeric id must fail")
	}
}
