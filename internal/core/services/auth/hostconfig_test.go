package auth

import "testing"

func TestHostConfig_SSH(t *testing.T) {
	cfg := map[string]string{"gitlab.mydomain.com": "ssh"}
	parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
	d, ok := tryHostConfig(parsed, cfg)
	if !ok {
		t.Fatal("expected match")
	}
	if d.Transport != TransportSSH {
		t.Errorf("Transport = %v", d.Transport)
	}
	if d.URL != "git@gitlab.mydomain.com:g/r" {
		t.Errorf("URL = %q", d.URL)
	}
}

func TestHostConfig_HTTPS(t *testing.T) {
	cfg := map[string]string{"github.com": "https"}
	parsed := ParsedURL{Host: "github.com", Path: "x/y"}
	d, ok := tryHostConfig(parsed, cfg)
	if !ok {
		t.Fatal("expected match")
	}
	if d.Transport != TransportHTTPS || d.URL != "https://github.com/x/y" {
		t.Errorf("got %+v", d)
	}
}

func TestHostConfig_Missing(t *testing.T) {
	parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
	if _, ok := tryHostConfig(parsed, nil); ok {
		t.Fatal("nil map must not match")
	}
	if _, ok := tryHostConfig(parsed, map[string]string{"other.host": "ssh"}); ok {
		t.Fatal("non-matching host must not match")
	}
}

func TestHostConfig_InvalidValue(t *testing.T) {
	parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
	if _, ok := tryHostConfig(parsed, map[string]string{"gitlab.mydomain.com": "bogus"}); ok {
		t.Fatal("invalid value must not match (caller falls through)")
	}
}
