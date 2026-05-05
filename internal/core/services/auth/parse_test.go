package auth

import "testing"

func TestParseDepURL(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantKind      URLKind
		wantHost      string
		wantPath      string
		wantCanonical string
	}{
		{
			name:          "bare name",
			input:         "horse",
			wantKind:      URLKindBare,
			wantHost:      "",
			wantPath:      "horse",
			wantCanonical: "horse",
		},
		{
			name:          "host slash path",
			input:         "gitlab.mydomain.com/group/repo",
			wantKind:      URLKindHostPath,
			wantHost:      "gitlab.mydomain.com",
			wantPath:      "group/repo",
			wantCanonical: "gitlab.mydomain.com/group/repo",
		},
		{
			name:          "git@ ssh form",
			input:         "git@gitlab.mydomain.com:group/repo.git",
			wantKind:      URLKindSSH,
			wantHost:      "gitlab.mydomain.com",
			wantPath:      "group/repo",
			wantCanonical: "gitlab.mydomain.com/group/repo",
		},
		{
			name:          "git@ ssh form without .git",
			input:         "git@gitlab.mydomain.com:group/repo",
			wantKind:      URLKindSSH,
			wantHost:      "gitlab.mydomain.com",
			wantPath:      "group/repo",
			wantCanonical: "gitlab.mydomain.com/group/repo",
		},
		{
			name:          "https url",
			input:         "https://github.com/HashLoad/horse.git",
			wantKind:      URLKindHTTPS,
			wantHost:      "github.com",
			wantPath:      "HashLoad/horse",
			wantCanonical: "github.com/HashLoad/horse",
		},
		{
			name:          "http url",
			input:         "http://gitlab.mydomain.com/group/repo",
			wantKind:      URLKindHTTPS,
			wantHost:      "gitlab.mydomain.com",
			wantPath:      "group/repo",
			wantCanonical: "gitlab.mydomain.com/group/repo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDepURL(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, tt.wantHost)
			}
			if got.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", got.Path, tt.wantPath)
			}
			if got.Canonical != tt.wantCanonical {
				t.Errorf("Canonical = %q, want %q", got.Canonical, tt.wantCanonical)
			}
		})
	}
}

func TestParseDepURL_Invalid(t *testing.T) {
	cases := []string{"", "git@no-colon-form", "https://"}
	for _, c := range cases {
		if _, err := ParseDepURL(c); err == nil {
			t.Errorf("ParseDepURL(%q) returned no error", c)
		}
	}
}
