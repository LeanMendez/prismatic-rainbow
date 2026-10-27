package gitctx

import "testing"

func TestParseRemoteURL(t *testing.T) {
	for raw, want := range map[string][2]string{
		"git@bitbucket.org:acme/api.git":                  {"acme", "api"},
		"git@ssh.bitbucket.org:acme/api.git":              {"acme", "api"},
		"ssh://git@ssh.bitbucket.org/acme/api.git":        {"acme", "api"},
		"https://juan@bitbucket.org/acme/api.git":         {"acme", "api"},
		"https://x-token-auth:tok@bitbucket.org/acme/api": {"acme", "api"},
		"https://bitbucket.org/acme/api/\n":               {"acme", "api"},
	} {
		ws, repo, err := ParseRemoteURL(raw)
		if err != nil || ws != want[0] || repo != want[1] {
			t.Errorf("ParseRemoteURL(%q) = %q %q %v", raw, ws, repo, err)
		}
	}
	for _, raw := range []string{"git@github.com:acme/api.git", "https://bitbucket.org/acme", "/local/path"} {
		if _, _, err := ParseRemoteURL(raw); err == nil {
			t.Errorf("ParseRemoteURL(%q) should fail", raw)
		}
	}
}
