package pr

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/config"
	"github.com/LeanMendez/prismatic-rainbow/internal/gitctx"
	"github.com/LeanMendez/prismatic-rainbow/internal/iostreams"
)

// Fixtures are real API responses for atlassian/amps PR #1255 (merged)
// and the mergeability checks of PR #1251 (open).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../api/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func diffFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../diffparse/testdata/pr1255.diff")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// routes maps a path suffix under /repositories/acme/api/pullrequests/1255
// to a handler.
type routes map[string]http.HandlerFunc

func serve(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.Write(body) }
}

func runPR(t *testing.T, r routes, args ...string) (stdout string, err error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		const prefix = "/repositories/acme/api/pullrequests/1255"
		suffix := strings.TrimPrefix(req.URL.Path, prefix)
		if h, ok := r[suffix]; ok && strings.HasPrefix(req.URL.Path, prefix) {
			h(w, req)
			return
		}
		if h, ok := r[req.URL.Path]; ok {
			h(w, req)
			return
		}
		t.Errorf("unexpected request %s", req.URL.Path)
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)

	ios, _, out, _ := iostreams.Test()
	f := &factory.Factory{
		IO:      ios,
		Options: &factory.GlobalOptions{Workspace: "acme", Repo: "api", Output: "json"},
		Config: func() (*config.Resolved, error) {
			return &config.Resolved{Token: "t", ClickUpCustomIDPrefixes: []string{"AMPSDEV"}}, nil
		},
		Client: func() (*api.Client, error) {
			c := api.NewClient(srv.URL, "", "t", "test")
			c.Sleep = func(time.Duration) {}
			return c, nil
		},
		GitContext: func() (*gitctx.Context, error) { return nil, gitctx.ErrNotBitbucketRepo },
	}
	cmd := NewCmdPR(f)
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err = cmd.Execute()
	return out.String(), err
}

func TestContextBundlesSectionsAndDegradesOnFailure(t *testing.T) {
	out, err := runPR(t, routes{
		"": serve(fixture(t, "pr.json")),
		"/statuses": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"message":"scopes","detail":{"required":["read:pipeline:bitbucket"]}}}`))
		},
		"/mergeability/checks": serve(fixture(t, "merge_open.json")),
		"/diffstat":            serve(fixture(t, "diffstat.json")),
		"/commits":             serve(fixture(t, "commits.json")),
		"/comments":            serve(fixture(t, "comments.json")),
		"/tasks":               serve(fixture(t, "tasks.json")),
		"/diff":                serve(diffFixture(t)),
	}, "context", "1255", "--diff", "--exclude", "*Test.java", "--exclude", "**/test/**")
	if err != nil {
		t.Fatal(err)
	}
	var v ContextView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}

	if v.Checks != nil {
		t.Error("failed section must be null")
	}
	if len(v.Warnings) != 1 || v.Warnings[0].Section != "checks" || v.Warnings[0].Code != "missing_scopes" {
		t.Errorf("warnings = %+v", v.Warnings)
	}
	if v.PullRequest.State != "MERGED" || v.PullRequest.Review.Approvals != 3 {
		t.Errorf("pull_request = %+v", v.PullRequest)
	}
	if v.Files == nil || v.Files.Count != 14 || v.Tasks == nil || v.Tasks.Total != 2 || len(v.Commits) != 3 {
		t.Errorf("files=%+v tasks=%+v commits=%d", v.Files, v.Tasks, len(v.Commits))
	}
	if v.Mergeability == nil || v.Mergeability.Mergeable || v.Mergeability.MergeableIgnoringPermission {
		t.Errorf("mergeability = %+v", v.Mergeability)
	}
	if v.Diff == nil || v.Diff.TotalFiles != 14 {
		t.Fatalf("diff = %+v", v.Diff)
	}
	for _, f := range v.Diff.Files {
		if strings.Contains(f.Path, "/test/") {
			t.Errorf("excluded file present: %s", f.Path)
		}
	}
	// AMPSDEV is a configured custom prefix; it appears in title, branch,
	// description and commits.
	if len(v.TaskReferences) != 1 || v.TaskReferences[0].ID != "AMPSDEV-373" {
		t.Fatalf("task_references = %+v", v.TaskReferences)
	}
	sources := map[string]bool{}
	for _, l := range v.TaskReferences[0].FoundIn {
		sources[l.Source] = true
	}
	for _, s := range []string{"title", "branch", "description", "commit"} {
		if !sources[s] {
			t.Errorf("reference not found in %s: %+v", s, v.TaskReferences[0].FoundIn)
		}
	}
}

func TestThreadsGroupRepliesUnderRoot(t *testing.T) {
	var page struct{ Values []api.Comment }
	if err := json.Unmarshal(fixture(t, "comments.json"), &page); err != nil {
		t.Fatal(err)
	}
	v := newThreadsView(page.Values)
	// 10 comments: 7 roots; 2011386118 replies to a reply of 2011337003.
	if v.Total != 7 || v.Unresolved != 7 {
		t.Fatalf("total=%d unresolved=%d", v.Total, v.Unresolved)
	}
	for _, th := range v.Threads {
		if th.ID == 2011337003 {
			if len(th.Comments) != 3 || th.Path != "amps-maven-plugin/src/main/java/com/atlassian/maven/plugins/amps/util/ZipUtils.java" || *th.Line != 83 {
				t.Errorf("thread = %+v", th)
			}
			return
		}
	}
	t.Error("thread 2011337003 not found")
}

func TestChecksExitCodes(t *testing.T) {
	statuses := func(states ...string) []byte {
		var vals []string
		for _, s := range states {
			vals = append(vals, `{"key":"k-`+s+`","name":"build `+s+`","state":"`+s+`"}`)
		}
		return []byte(`{"values":[` + strings.Join(vals, ",") + `]}`)
	}
	for _, tc := range []struct {
		states   []string
		wantCode int
		overall  string
	}{
		{[]string{"SUCCESSFUL"}, 0, BucketPass},
		{[]string{"SUCCESSFUL", "INPROGRESS"}, 8, BucketPending},
		{[]string{"INPROGRESS", "FAILED"}, 1, BucketFail},
		{nil, 0, BucketNone},
	} {
		out, err := runPR(t, routes{
			"/statuses": serve(statuses(tc.states...)),
		}, "checks", "1255")
		code := 0
		var exitErr *cmdutil.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		} else if err != nil {
			t.Fatal(err)
		}
		var v ChecksView
		if jerr := json.Unmarshal([]byte(out), &v); jerr != nil {
			t.Fatalf("%v: invalid JSON %q", tc.states, out)
		}
		if code != tc.wantCode || v.Overall != tc.overall {
			t.Errorf("%v: code=%d overall=%s, want %d %s", tc.states, code, v.Overall, tc.wantCode, tc.overall)
		}
		if len(v.Statuses) > 0 && tc.overall == BucketFail && v.Statuses[0].Bucket != BucketFail {
			t.Errorf("failures must be listed first: %+v", v.Statuses)
		}
	}
}

func TestFileReadsSourceCommitOfForkWithLineRange(t *testing.T) {
	pr := []byte(`{"id":1255,"source":{"branch":{"name":"fix"},"commit":{"hash":"abc123abc123"},"repository":{"full_name":"someone/api-fork"}},
		"destination":{"branch":{"name":"main"},"commit":{"hash":"def456def456"},"repository":{"full_name":"acme/api"}}}`)
	out, err := runPR(t, routes{
		"": serve(pr),
		"/repositories/someone/api-fork/src/abc123abc123/src/app.go": serve([]byte("l1\nl2\nl3\nl4\n")),
	}, "file", "1255", "src/app.go", "--lines", "2:3")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Commit     string `json:"commit"`
		Content    string `json:"content"`
		TotalLines int    `json:"total_lines"`
		StartLine  int    `json:"start_line"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.Commit != "abc123abc123" || v.Content != "l2\nl3\n" || v.TotalLines != 4 || v.StartLine != 2 {
		t.Errorf("got %+v", v)
	}
}

func TestListBuildsBBQLFromFilters(t *testing.T) {
	var gotQ, gotStates string
	_, err := runPR(t, routes{
		"/repositories/acme/api/pullrequests": func(w http.ResponseWriter, r *http.Request) {
			gotQ = r.URL.Query().Get("q")
			gotStates = strings.Join(r.URL.Query()["state"], ",")
			w.Write([]byte(`{"values":[]}`))
		},
	}, "list", "--state", "open,merged", "--dest", "develop", "--task", `CU-86b"1`)
	if err != nil {
		t.Fatal(err)
	}
	wantQ := `destination.branch.name = "develop" AND (title ~ "CU-86b\"1" OR source.branch.name ~ "CU-86b\"1" OR description ~ "CU-86b\"1")`
	if gotQ != wantQ || gotStates != "OPEN,MERGED" {
		t.Errorf("q = %s\nstates = %s", gotQ, gotStates)
	}
}
