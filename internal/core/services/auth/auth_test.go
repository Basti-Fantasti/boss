package auth

import (
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

func TestTransportString(t *testing.T) {
	if TransportSSH.String() != "ssh" {
		t.Errorf("TransportSSH.String() = %q, want \"ssh\"", TransportSSH.String())
	}
	if TransportHTTPS.String() != "https" {
		t.Errorf("TransportHTTPS.String() = %q, want \"https\"", TransportHTTPS.String())
	}
}

func TestResolve_DefaultHTTPS(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	// No env, no stored creds, no host config → default HTTPS, no auth.
	dep := domain.Dependency{Repository: "github.com/HashLoad/horse"}
	d, err := ResolveWith(dep, fakeStore{}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.URL != "https://github.com/HashLoad/horse" {
		t.Errorf("URL = %q", d.URL)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("Transport = %v", d.Transport)
	}
	if d.Layer != "default" {
		t.Errorf("Layer = %q", d.Layer)
	}
}

func TestResolve_BareName_DefaultOrg(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	dep := domain.Dependency{Repository: "horse"}
	d, err := ResolveWith(dep, fakeStore{}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.URL != "https://github.com/hashload/horse" {
		t.Errorf("URL = %q (bare names default to upstream org)", d.URL)
	}
}

func TestResolve_GitLabCI_BeatsExplicitSSH(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
	t.Setenv("CI_JOB_TOKEN", "tok")
	dep := domain.Dependency{Repository: "git@gitlab.mydomain.com:g/r.git"}
	d, err := ResolveWith(dep, fakeStore{}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("CI must coerce SSH form to HTTPS; got %v", d.Transport)
	}
	if d.URL != "https://gitlab.mydomain.com/g/r" {
		t.Errorf("URL = %q", d.URL)
	}
	if d.Credential.User != "gitlab-ci-token" || d.Credential.Password != "tok" {
		t.Errorf("Credential = %+v, want the job token carried as a credential", d.Credential)
	}
	if d.Layer != "gitlab-ci" {
		t.Errorf("Layer = %q", d.Layer)
	}
}

func TestResolve_ExplicitSSH_NoCI(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	dep := domain.Dependency{Repository: "git@gitlab.mydomain.com:g/r.git"}
	d, err := ResolveWith(dep, fakeStore{}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.Transport != TransportSSH {
		t.Errorf("Transport = %v, want SSH", d.Transport)
	}
	if d.URL != "git@gitlab.mydomain.com:g/r" {
		t.Errorf("URL = %q", d.URL)
	}
	if d.Layer != "explicit" {
		t.Errorf("Layer = %q", d.Layer)
	}
}

func TestResolve_Stored_BeatsDefault(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	dep := domain.Dependency{Repository: "gitlab.mydomain.com/g/r"}
	store := fakeStore{"gitlab.mydomain.com": {User: "alice", Pass: "secret"}}
	d, err := ResolveWith(dep, store, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.Layer != "stored" || d.Credential.User != "alice" {
		t.Errorf("got %+v", d)
	}
}
