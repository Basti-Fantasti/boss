package auth

import (
	"fmt"
	"strings"
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
	// Asserted field by field: CredentialSpec masks its password when
	// formatted, so a %+v of the whole struct cannot show what went wrong.
	if d.Credential.User != "gitlab-ci-token" {
		t.Errorf("Credential.User = %q, want %q", d.Credential.User, "gitlab-ci-token")
	}
	if d.Credential.Password != "tok" {
		t.Errorf("Credential.Password = %q, want the job token %q", d.Credential.Password, "tok")
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

// TestCredentialSpec_MasksPasswordInFormattedOutput locks the property that
// keeps the GitLab CI job token out of logs and errors. The token moved out of
// Decision.URL - where redactURLCredentials masks it on the way into any log -
// and into a plain struct field, so the masking has to live on the struct.
//
// Every verb that reaches for a value's own representation is covered: %v and
// %+v go through String, %s does too, and %#v goes through GoString instead
// and would otherwise dump the fields verbatim.
func TestCredentialSpec_MasksPasswordInFormattedOutput(t *testing.T) {
	const token = "glcbt-XyZzy-9f3a1c-NOT-FOR-LOGS"
	spec := CredentialSpec{User: "gitlab-ci-token", Password: token}

	for _, format := range []string{"%v", "%+v", "%s", "%#v"} {
		got := fmt.Sprintf(format, spec)
		if strings.Contains(got, token) {
			t.Errorf("%s of a CredentialSpec leaked the password: %s", format, got)
		}
		// The user is not secret and is what identifies the resolution layer,
		// so masking must not have swallowed the whole struct.
		if !strings.Contains(got, "gitlab-ci-token") {
			t.Errorf("%s of a CredentialSpec dropped the user: %s", format, got)
		}
		if !strings.Contains(got, "***") {
			t.Errorf("%s of a CredentialSpec does not show a masked password: %s", format, got)
		}
	}

	// A Decision has no String of its own; the field's masking must still apply
	// when the whole decision is formatted, which is the shape a debug line
	// would most plausibly take.
	decision := Decision{
		URL:        "https://gitlab.mydomain.com/g/r",
		Transport:  TransportHTTPS,
		Credential: spec,
		Layer:      "gitlab-ci",
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		got := fmt.Sprintf(format, decision)
		if strings.Contains(got, token) {
			t.Errorf("%s of a Decision leaked the credential password: %s", format, got)
		}
		if !strings.Contains(got, "gitlab.mydomain.com") {
			t.Errorf("%s of a Decision no longer shows the URL: %s", format, got)
		}
	}

	// An absent credential must not be reported as a withheld one.
	if got := fmt.Sprintf("%v", CredentialSpec{}); strings.Contains(got, "***") {
		t.Errorf("the zero CredentialSpec formats as if it held a secret: %s", got)
	}
}
