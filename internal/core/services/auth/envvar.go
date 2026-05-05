package auth

import (
	"os"
	"strings"
)

// envVarKey returns the env var name bossy looks up for a given host.
// "gitlab.mydomain.com" → "BOSSY_AUTH_GITLAB_MYDOMAIN_COM".
func envVarKey(host string) string {
	upper := strings.ToUpper(host)
	upper = strings.ReplaceAll(upper, ".", "_")
	upper = strings.ReplaceAll(upper, "-", "_")
	return "BOSSY_AUTH_" + upper
}

// tryEnvVar implements layer 2: BOSSY_AUTH_<HOST> override.
//
// Supported formats:
//
//	https-token:<user>:<token>   → HTTPS basic auth
//	ssh                          → force SSH (system git uses agent/config)
func tryEnvVar(parsed ParsedURL) (Decision, bool) {
	if parsed.Host == "" {
		return Decision{}, false
	}
	raw := os.Getenv(envVarKey(parsed.Host))
	if raw == "" {
		return Decision{}, false
	}

	if raw == "ssh" {
		return Decision{
			URL:       "git@" + parsed.Host + ":" + parsed.Path,
			Transport: TransportSSH,
			Layer:     "envvar",
		}, true
	}

	if rest, ok := strings.CutPrefix(raw, "https-token:"); ok {
		idx := strings.Index(rest, ":")
		if idx <= 0 || idx == len(rest)-1 {
			return Decision{}, false
		}
		return Decision{
			URL:        "https://" + parsed.Host + "/" + parsed.Path,
			Transport:  TransportHTTPS,
			Credential: CredentialSpec{User: rest[:idx], Password: rest[idx+1:]},
			Layer:      "envvar",
		}, true
	}

	return Decision{}, false
}
