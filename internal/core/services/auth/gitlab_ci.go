package auth

import "os"

// tryGitLabCI implements the highest-priority resolution layer:
// when running under a GitLab Runner with a CI job token, rewrite
// any dep URL on the same GitLab instance to authenticated HTTPS.
//
// All three env vars (GITLAB_CI, CI_SERVER_HOST, CI_JOB_TOKEN) must
// be non-empty AND the dep host must match CI_SERVER_HOST. Missing
// any of them means we skip this layer rather than produce a
// half-formed URL.
func tryGitLabCI(parsed ParsedURL) (Decision, bool) {
	if os.Getenv("GITLAB_CI") != "true" {
		return Decision{}, false
	}
	serverHost := os.Getenv("CI_SERVER_HOST")
	token := os.Getenv("CI_JOB_TOKEN")
	if serverHost == "" || token == "" {
		return Decision{}, false
	}
	if parsed.Host == "" || parsed.Host != serverHost {
		return Decision{}, false
	}
	url := "https://gitlab-ci-token:" + token + "@" + parsed.Host + "/" + parsed.Path
	return Decision{
		URL:        url,
		Transport:  TransportHTTPS,
		Credential: CredentialSpec{}, // baked into URL
		Layer:      "gitlab-ci",
	}, true
}
