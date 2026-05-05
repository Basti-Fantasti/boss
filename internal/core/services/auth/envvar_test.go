package auth

import "testing"

func TestEnvVarHTTPSToken(t *testing.T) {
	t.Setenv("BOSSY_AUTH_GITLAB_MYDOMAIN_COM", "https-token:gitlab-ci-token:abc123")
	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
	d, ok := tryEnvVar(parsed)
	if !ok {
		t.Fatal("expected env var layer to match")
	}
	if d.URL != "https://gitlab.mydomain.com/g/r" {
		t.Errorf("URL = %q", d.URL)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("Transport = %v", d.Transport)
	}
	if d.Credential.User != "gitlab-ci-token" || d.Credential.Password != "abc123" {
		t.Errorf("Credential = %+v", d.Credential)
	}
}

func TestEnvVarSSH(t *testing.T) {
	t.Setenv("BOSSY_AUTH_GITLAB_MYDOMAIN_COM", "ssh")
	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
	d, ok := tryEnvVar(parsed)
	if !ok {
		t.Fatal("expected env var layer to match")
	}
	if d.Transport != TransportSSH {
		t.Errorf("Transport = %v, want SSH", d.Transport)
	}
	if d.URL != "git@gitlab.mydomain.com:g/r" {
		t.Errorf("URL = %q", d.URL)
	}
}

func TestEnvVar_Missing(t *testing.T) {
	parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r"}
	if _, ok := tryEnvVar(parsed); ok {
		t.Fatal("expected no match when env unset")
	}
}

func TestEnvVar_BareDep_NoMatch(t *testing.T) {
	t.Setenv("BOSSY_AUTH_HORSE", "ssh")
	parsed := ParsedURL{Kind: URLKindBare, Path: "horse"}
	if _, ok := tryEnvVar(parsed); ok {
		t.Fatal("bare deps have no host")
	}
}

func TestEnvVarHostKey(t *testing.T) {
	cases := map[string]string{
		"gitlab.mydomain.com": "BOSSY_AUTH_GITLAB_MYDOMAIN_COM",
		"github.com":    "BOSSY_AUTH_GITHUB_COM",
		"git.example":   "BOSSY_AUTH_GIT_EXAMPLE",
	}
	for host, want := range cases {
		if got := envVarKey(host); got != want {
			t.Errorf("envVarKey(%q) = %q, want %q", host, got, want)
		}
	}
}
