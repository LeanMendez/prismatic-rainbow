package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testClient(t *testing.T, h http.HandlerFunc, email string) (*Client, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, email, "secret", "test")
	var sleeps []time.Duration
	c.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	return c, &sleeps
}

func TestAuthModes(t *testing.T) {
	for _, tc := range []struct {
		email, want string
	}{
		{"me@acme.com", "Basic "},
		{"", "Bearer secret"},
	} {
		var got string
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Authorization")
			w.Write([]byte(`{"display_name":"Me"}`))
		}, tc.email)
		if _, err := c.CurrentUser(); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("email %q: Authorization = %q, want prefix %q", tc.email, got, tc.want)
		}
	}
}

func TestPaginateFollowsNextAndStopsAtLimit(t *testing.T) {
	var calls int
	var srvURL string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`{"values":[{"id":3},{"id":4}],"next":"` + srvURL + `/x?page=3"}`))
			return
		}
		// pagelen shrinks to the limit so small requests fetch less.
		if got := r.URL.Query().Get("pagelen"); got != "3" {
			t.Errorf("pagelen = %q, want 3", got)
		}
		w.Write([]byte(`{"values":[{"id":1},{"id":2}],"next":"` + srvURL + `/x?page=2"}`))
	}, "")
	srvURL = c.BaseURL

	prs, err := c.ListPullRequests("acme", "api", ListPullRequestsOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 || prs[2].ID != 3 {
		t.Fatalf("got %+v, want ids 1..3", prs)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (must not fetch page 3)", calls)
	}
}

func TestScopeErrorExposesRequiredScopes(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"type":"error","error":{"message":"Your credentials lack one or more required privilege scopes.","detail":{"granted":["read:repository:bitbucket"],"required":["read:pullrequest:bitbucket"]}}}`))
	}, "")
	_, err := c.GetPullRequest("acme", "api", 1)
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if apiErr.Code() != "missing_scopes" || len(apiErr.RequiredScopes) != 1 || apiErr.RequiredScopes[0] != "read:pullrequest:bitbucket" {
		t.Errorf("got code %q scopes %v", apiErr.Code(), apiErr.RequiredScopes)
	}
	if !strings.Contains(err.Error(), "read:pullrequest:bitbucket") {
		t.Errorf("message should name the missing scope: %v", err)
	}
}

func TestGetRetriesRateLimitHonoringRetryAfter(t *testing.T) {
	var calls int
	c, sleeps := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			return
		}
		w.Write(fixture(t, "pr.json"))
	}, "")
	pr, err := c.GetPullRequest("atlassian", "amps", 1255)
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != 1255 || calls != 2 {
		t.Fatalf("id=%d calls=%d", pr.ID, calls)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != 7*time.Second {
		t.Errorf("sleeps = %v, want [7s]", *sleeps)
	}
}

func TestRetriesAreBoundedAndWritesAreNotRetried(t *testing.T) {
	var calls int
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(503)
	}, "")
	if _, err := c.GetPullRequest("a", "b", 1); err == nil {
		t.Fatal("want error")
	}
	if calls != c.MaxRetries+1 {
		t.Errorf("GET calls = %d, want %d", calls, c.MaxRetries+1)
	}
	calls = 0
	if _, err := c.MergePullRequest("a", "b", 1, MergeInput{}); err == nil {
		t.Fatal("want error")
	}
	if calls != 1 {
		t.Errorf("POST calls = %d, want 1 (merges must never be retried)", calls)
	}
}

func TestDiffFollowsRedirectKeepingAuthAndOptions(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/pullrequests/7/diff") {
			http.Redirect(w, r, "/repositories/a/b/diff/x:abc%0Ddef?"+r.URL.RawQuery+"&topic=true", http.StatusFound)
			return
		}
		if r.URL.Query().Get("context") != "0" || r.URL.Query().Get("ignore_whitespace") != "true" {
			t.Errorf("options lost in redirect: %s", r.URL.RawQuery)
		}
		w.Write([]byte("diff --git a/x b/x\n"))
	}, "")
	d, err := c.GetPullRequestDiff("a", "b", 7, DiffOptions{Context: 0, IgnoreWhitespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if d != "diff --git a/x b/x\n" {
		t.Errorf("diff = %q", d)
	}
}

func TestResolveRef(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/repositories/a/b/refs/branches/feature%2FCU-86b1_login":
			w.Write([]byte(`{"name":"feature/CU-86b1_login","target":{"hash":"1111111111111111111111111111111111111111"}}`))
		case "/repositories/a/b/refs/branches/v1.0":
			w.WriteHeader(404)
			w.Write([]byte(`{"type":"error","error":{"message":"not found"}}`))
		case "/repositories/a/b/refs/tags/v1.0":
			w.Write([]byte(`{"name":"v1.0","target":{"hash":"2222222222222222222222222222222222222222"}}`))
		default:
			t.Errorf("unexpected request %s", r.URL.EscapedPath())
			w.WriteHeader(500)
		}
	}, "")
	for ref, want := range map[string]string{
		"e24b8a7a25dc":          "e24b8a7a25dc",
		"feature/CU-86b1_login": "1111111111111111111111111111111111111111",
		"v1.0":                  "2222222222222222222222222222222222222222",
	} {
		got, err := c.ResolveRef("a", "b", ref)
		if err != nil || got != want {
			t.Errorf("ResolveRef(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
}

func TestDecodeRealPullRequest(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "pr.json")) }, "")
	pr, err := c.GetPullRequest("atlassian", "amps", 1255)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != StateMerged || pr.Source.CommitHash() != "e24b8a7a25dc" || pr.Destination.Branch.Name != "master" ||
		pr.Source.RepoFullName() != "atlassian/amps" || pr.ClosedBy.Name() != "Frederic Ghilini" || len(pr.Participants) != 4 {
		t.Errorf("unexpected decode: %+v", pr)
	}
}
