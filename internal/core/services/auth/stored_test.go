package auth

import "testing"

// fakeStore is a test double for the credential store interface.
type fakeStore map[string]struct{ User, Pass string }

func (f fakeStore) GetHTTPSCredentials(host string) (user, pass string, ok bool) {
	c, present := f[host]
	if !present {
		return "", "", false
	}
	return c.User, c.Pass, true
}

func TestStored_Match(t *testing.T) {
	s := fakeStore{"gitlab.mydomain.com": {User: "alice", Pass: "hunter2"}}
	parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
	d, ok := tryStored(parsed, s)
	if !ok {
		t.Fatal("expected stored layer to match")
	}
	if d.URL != "https://gitlab.mydomain.com/g/r" || d.Transport != TransportHTTPS {
		t.Errorf("Decision = %+v", d)
	}
	// Asserted field by field: CredentialSpec masks its password when
	// formatted, so a %+v of the whole struct cannot show what went wrong.
	if d.Credential.User != "alice" {
		t.Errorf("Credential.User = %q, want %q", d.Credential.User, "alice")
	}
	if d.Credential.Password != "hunter2" {
		t.Errorf("Credential.Password = %q, want %q", d.Credential.Password, "hunter2")
	}
}

func TestStored_NoMatch(t *testing.T) {
	parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
	if _, ok := tryStored(parsed, fakeStore{}); ok {
		t.Fatal("expected no match")
	}
}

func TestStored_BareDep(t *testing.T) {
	parsed := ParsedURL{Kind: URLKindBare, Path: "horse"}
	s := fakeStore{"github.com": {User: "u", Pass: "p"}}
	if _, ok := tryStored(parsed, s); ok {
		t.Fatal("bare deps have no host; layer cannot match")
	}
}
