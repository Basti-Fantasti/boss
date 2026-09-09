package auth

import (
	"strings"
	"testing"
)

func TestGitLabCI_Match(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "secret-token")

	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "group/repo", Canonical: "gitlab.mydomain.com/group/repo"}
	d, ok := tryGitLabCI(parsed)
	if !ok {
		t.Fatal("expected gitlab CI layer to match")
	}
	want := "https://gitlab.mydomain.com/group/repo"
	if d.URL != want {
		t.Errorf("URL = %q, want %q", d.URL, want)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("Transport = %v, want HTTPS", d.Transport)
	}
	if d.Layer != "gitlab-ci" {
		t.Errorf("Layer = %q, want \"gitlab-ci\"", d.Layer)
	}
	// The token must travel as a credential, not in the URL: go-git writes the
	// clone URL into the cache's .git/config, where it would outlive the job.
	if strings.Contains(d.URL, "secret-token") {
		t.Errorf("job token leaked into URL: %q", d.URL)
	}
	if d.Credential.User != "gitlab-ci-token" {
		t.Errorf("Credential.User = %q, want \"gitlab-ci-token\"", d.Credential.User)
	}
	if d.Credential.Password != "secret-token" {
		t.Errorf("Credential.Password = %q, want the job token", d.Credential.Password)
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
	if d.URL != "https://gitlab.mydomain.com/group/repo" {
		t.Errorf("unexpected URL: %s", d.URL)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("Transport = %v, want HTTPS", d.Transport)
	}
	if d.Layer != "gitlab-ci" {
		t.Errorf("Layer = %q, want \"gitlab-ci\"", d.Layer)
	}
	if strings.Contains(d.URL, "tok") {
		t.Errorf("job token leaked into URL: %q", d.URL)
	}
	// Asserted field by field: CredentialSpec masks its password when
	// formatted, so a %+v of the whole struct cannot show what went wrong.
	if d.Credential.User != "gitlab-ci-token" {
		t.Errorf("Credential.User = %q, want %q", d.Credential.User, "gitlab-ci-token")
	}
	if d.Credential.Password != "tok" {
		t.Errorf("Credential.Password = %q, want the job token %q", d.Credential.Password, "tok")
	}
}

// TestGitLabCI_TokenNeverInURL pins the security property on its own, with a
// token distinctive enough that no substring of the host or path could mask a
// leak. Decision.URL is what go-git persists to disk; the secret must not be
// anywhere in it.
func TestGitLabCI_TokenNeverInURL(t *testing.T) {
	const token = "glcbt-XyZzy-9f3a1c-NOT-FOR-DISK"
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", token)

	for _, kind := range []URLKind{URLKindHostPath, URLKindSSH} {
		parsed := ParsedURL{
			Kind:      kind,
			Host:      "gitlab.mydomain.com",
			Path:      "group/repo",
			Canonical: "gitlab.mydomain.com/group/repo",
		}
		d, ok := tryGitLabCI(parsed)
		if !ok {
			t.Fatalf("kind %v: expected gitlab CI layer to match", kind)
		}
		if strings.Contains(d.URL, token) {
			t.Errorf("kind %v: token present in Decision.URL %q", kind, d.URL)
		}
		if d.Credential.Password != token {
			t.Errorf("kind %v: Credential.Password = %q, want the job token", kind, d.Credential.Password)
		}
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
