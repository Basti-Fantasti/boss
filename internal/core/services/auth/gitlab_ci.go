package auth

import "os"

// tryGitLabCI implements the highest-priority resolution layer:
// when running under a GitLab Runner with a CI job token, rewrite
// any dep URL on the same GitLab instance to HTTPS with the job
// token supplied as a credential.
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
	// The token goes in the credential, never the URL. go-git persists the
	// clone URL into the cache's .git/config, so a token embedded here would be
	// written to disk and then reused — expired — by the next CI job.
	return Decision{
		URL:        "https://" + parsed.Host + "/" + parsed.Path,
		Transport:  TransportHTTPS,
		Credential: CredentialSpec{User: "gitlab-ci-token", Password: token},
		Layer:      "gitlab-ci",
	}, true
}
