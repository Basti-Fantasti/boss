package domain_test

import (
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

func TestDependency_Name(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		expected   string
	}{
		{
			name:       "github repository",
			repository: "github.com/basti-fantasti/bossy",
			expected:   "bossy",
		},
		{
			name:       "gitlab repository",
			repository: "gitlab.com/user/project",
			expected:   "project",
		},
		{
			name:       "bitbucket repository",
			repository: "bitbucket.org/team/repo",
			expected:   "repo",
		},
		{
			name:       "nested path repository",
			repository: "github.com/org/group/subgroup/repo",
			expected:   "repo",
		},
		{
			name:       "repository with trailing slash",
			repository: "github.com/basti-fantasti/bossy/",
			expected:   "bossy/",
		},
		{
			name:       "simple name",
			repository: "simple-repo",
			expected:   "simple-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := domain.Dependency{Repository: tt.repository}
			result := dep.Name()
			if result != tt.expected {
				t.Errorf("Name() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDependency_HashName(t *testing.T) {
	tests := []struct {
		name       string
		repository string
	}{
		{
			name:       "github repository",
			repository: "github.com/basti-fantasti/bossy",
		},
		{
			name:       "empty repository",
			repository: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := domain.Dependency{Repository: tt.repository}
			hash := dep.HashName()

			// MD5 hash should be 32 hex characters
			if len(hash) != 32 {
				t.Errorf("HashName() length = %d, want 32", len(hash))
			}

			// Same repository should produce same hash
			dep2 := domain.Dependency{Repository: tt.repository}
			hash2 := dep2.HashName()
			if hash != hash2 {
				t.Errorf("Same repository should produce same hash: got %s and %s", hash, hash2)
			}
		})
	}

	t.Run("different repositories produce different hashes", func(t *testing.T) {
		dep1 := domain.Dependency{Repository: "github.com/user/repo1"}
		dep2 := domain.Dependency{Repository: "github.com/user/repo2"}

		hash1 := dep1.HashName()
		hash2 := dep2.HashName()

		if hash1 == hash2 {
			t.Error("Different repositories should produce different hashes")
		}
	})
}

func TestDependency_GetVersion(t *testing.T) {
	tests := []struct {
		name     string
		info     string
		expected string
	}{
		{
			name:     "semantic version",
			info:     "1.0.0",
			expected: "1.0.0",
		},
		{
			name:     "caret version",
			info:     "^1.0.0",
			expected: "^1.0.0",
		},
		{
			name:     "tilde version",
			info:     "~1.0.0",
			expected: "~1.0.0",
		},
		{
			name:     "two part version gets .0 appended",
			info:     "1.0",
			expected: "1.0.0",
		},
		{
			name:     "single part version gets .0.0 appended",
			info:     "1",
			expected: "1.0.0",
		},
		{
			name:     "caret two part version",
			info:     "^1.0",
			expected: "^1.0.0",
		},
		{
			name:     "tilde single part version",
			info:     "~1",
			expected: "~1.0.0",
		},
		{
			name:     "branch name",
			info:     "main",
			expected: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := domain.ParseDependency("github.com/test/repo", tt.info)
			result := dep.GetVersion()
			if result != tt.expected {
				t.Errorf("GetVersion() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseDependency(t *testing.T) {
	tests := []struct {
		name         string
		repo         string
		info         string
		expectedRepo string
	}{
		{
			name:         "simple version",
			repo:         "github.com/basti-fantasti/bossy",
			info:         "1.0.0",
			expectedRepo: "github.com/basti-fantasti/bossy",
		},
		{
			name:         "version with colon suffix is ignored",
			repo:         "github.com/basti-fantasti/bossy",
			info:         "1.0.0:ssh",
			expectedRepo: "github.com/basti-fantasti/bossy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := domain.ParseDependency(tt.repo, tt.info)

			if dep.Repository != tt.expectedRepo {
				t.Errorf("Repository = %q, want %q", dep.Repository, tt.expectedRepo)
			}
		})
	}
}

func TestGetDependencies(t *testing.T) {
	tests := []struct {
		name     string
		deps     map[string]string
		expected int
	}{
		{
			name:     "empty map",
			deps:     map[string]string{},
			expected: 0,
		},
		{
			name: "single dependency",
			deps: map[string]string{
				"github.com/basti-fantasti/bossy": "1.0.0",
			},
			expected: 1,
		},
		{
			name: "multiple dependencies",
			deps: map[string]string{
				"github.com/basti-fantasti/bossy":  "1.0.0",
				"github.com/hashload/horse": "^2.0.0",
				"github.com/user/repo":      "~1.5.0",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := domain.GetDependencies(tt.deps)
			if len(result) != tt.expected {
				t.Errorf("GetDependencies() returned %d dependencies, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestGetDependenciesNames(t *testing.T) {
	deps := []domain.Dependency{
		{Repository: "github.com/basti-fantasti/bossy"},
		{Repository: "github.com/hashload/horse"},
		{Repository: "github.com/user/repo"},
	}

	names := domain.GetDependenciesNames(deps)

	if len(names) != 3 {
		t.Errorf("GetDependenciesNames() returned %d names, want 3", len(names))
	}

	expectedNames := []string{"bossy", "horse", "repo"}
	for i, expected := range expectedNames {
		if names[i] != expected {
			t.Errorf("GetDependenciesNames()[%d] = %q, want %q", i, names[i], expected)
		}
	}
}

func TestHashName_CanonicalForms(t *testing.T) {
	forms := []string{
		"gitlab.mydomain.com/group/repo",
		"git@gitlab.mydomain.com:group/repo",
		"git@gitlab.mydomain.com:group/repo.git",
		"https://gitlab.mydomain.com/group/repo",
		"https://gitlab.mydomain.com/group/repo.git",
		"GITLAB.MYDOMAIN.COM/group/repo", // case-insensitivity
	}
	var first string
	for i, f := range forms {
		d := domain.Dependency{Repository: f}
		h := d.HashName()
		if i == 0 {
			first = h
			continue
		}
		if h != first {
			t.Errorf("form %q has hash %s, want %s (must equal first form)", f, h, first)
		}
	}
}

func TestDependency_GetURLPrefix(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		expected   string
	}{
		{
			name:       "github.com",
			repository: "github.com/basti-fantasti/bossy",
			expected:   "github.com",
		},
		{
			name:       "gitlab.com",
			repository: "gitlab.com/user/repo",
			expected:   "gitlab.com",
		},
		{
			name:       "bitbucket.org",
			repository: "bitbucket.org/team/project",
			expected:   "bitbucket.org",
		},
		{
			name:       "custom domain",
			repository: "git.mycompany.com/team/repo",
			expected:   "git.mycompany.com",
		},
		{
			name:       "https url",
			repository: "https://github.com/user/repo",
			expected:   "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := domain.Dependency{Repository: tt.repository}
			result := dep.GetURLPrefix()
			if result != tt.expected {
				t.Errorf("GetURLPrefix() = %q, want %q", result, tt.expected)
			}
		})
	}
}

