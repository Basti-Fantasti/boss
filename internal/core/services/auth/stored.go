package auth

// CredentialStore returns stored HTTPS basic-auth credentials for a host.
// Implemented by env.Configuration in production and by test doubles
// in unit tests.
type CredentialStore interface {
	GetHTTPSCredentials(host string) (user, pass string, ok bool)
}

// tryStored implements layer 5: HTTPS credentials saved via `bossy auth set`.
func tryStored(parsed ParsedURL, store CredentialStore) (Decision, bool) {
	if parsed.Host == "" || store == nil {
		return Decision{}, false
	}
	user, pass, ok := store.GetHTTPSCredentials(parsed.Host)
	if !ok {
		return Decision{}, false
	}
	return Decision{
		URL:        "https://" + parsed.Host + "/" + parsed.Path,
		Transport:  TransportHTTPS,
		Credential: CredentialSpec{User: user, Password: pass},
		Layer:      "stored",
	}, true
}
