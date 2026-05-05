package auth

import "testing"

func TestGitLabCI_Match(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "secret-token")

	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "group/repo", Canonical: "gitlab.mydomain.com/group/repo"}
	d, ok := tryGitLabCI(parsed)
	if !ok {
		t.Fatal("expected gitlab CI layer to match")
	}
	want := "https://gitlab-ci-token:secret-token@gitlab.mydomain.com/group/repo"
	if d.URL != want {
		t.Errorf("URL = %q, want %q", d.URL, want)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("Transport = %v, want HTTPS", d.Transport)
	}
	if d.Layer != "gitlab-ci" {
		t.Errorf("Layer = %q, want \"gitlab-ci\"", d.Layer)
	}
}

func TestGitLabCI_OverridesSSHForm(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "tok")
	parsed := ParsedURL{Kind: URLKindSSH, Host: "gitlab.mydomain.com", Path: "group/repo", Canonical: "gitlab.mydomain.com/group/repo"}
	d, ok := tryGitLabCI(parsed)
	if !ok {
		t.Fatal("expected gitlab CI to override SSH form")
	}
	if d.URL != "https://gitlab-ci-token:tok@gitlab.mydomain.com/group/repo" {
		t.Errorf("unexpected URL: %s", d.URL)
	}
}

func TestGitLabCI_NotInCI(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
	if _, ok := tryGitLabCI(parsed); ok {
		t.Fatal("should not match outside CI")
	}
}

func TestGitLabCI_HostMismatch(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "tok")
	parsed := ParsedURL{Kind: URLKindHostPath, Host: "github.com", Path: "x/y", Canonical: "github.com/x/y"}
	if _, ok := tryGitLabCI(parsed); ok {
		t.Fatal("should not match for non-CI-server host")
	}
}

func TestGitLabCI_MissingToken_FallsThrough(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "")
	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
	if _, ok := tryGitLabCI(parsed); ok {
		t.Fatal("missing token must skip layer, not produce malformed URL")
	}
}

func TestGitLabCI_BareDep_NotMatched(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "tok")
	parsed := ParsedURL{Kind: URLKindBare, Path: "horse", Canonical: "horse"}
	if _, ok := tryGitLabCI(parsed); ok {
		t.Fatal("bare deps have no host, must skip CI layer")
	}
}
