package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{EnvEmail, EnvToken, EnvWorkspace, EnvCustomPrefixes} {
		t.Setenv(k, "")
	}
	if err := Save(&Config{Email: "file@acme.com", Token: "filetok", DefaultWorkspace: "filews", ClickUpCustomIDPrefixes: []string{"ENG"}}); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("config must be 0600: %v %v", st.Mode(), err)
	}
	if filepath.Base(filepath.Dir(path)) != AppName {
		t.Errorf("path = %s", path)
	}

	r, _ := Resolve(Overrides{})
	if r.Email != "file@acme.com" || r.Token != "filetok" || r.DefaultWorkspace != "filews" || r.ClickUpCustomIDPrefixes[0] != "ENG" {
		t.Errorf("file values: %+v", r)
	}

	// An env access token (no email) must not inherit the file's email,
	// otherwise it would be sent as Basic auth and fail.
	t.Setenv(EnvToken, "envtok")
	t.Setenv(EnvCustomPrefixes, "OPS, QA")
	r, _ = Resolve(Overrides{})
	if r.Token != "envtok" || r.Email != "" || len(r.ClickUpCustomIDPrefixes) != 2 || r.ClickUpCustomIDPrefixes[1] != "QA" {
		t.Errorf("env values: %+v", r)
	}

	r, _ = Resolve(Overrides{Token: "flagtok", Email: "flag@acme.com", Workspace: "flagws"})
	if r.Token != "flagtok" || r.Email != "flag@acme.com" || r.DefaultWorkspace != "flagws" {
		t.Errorf("flag values: %+v", r)
	}
}
