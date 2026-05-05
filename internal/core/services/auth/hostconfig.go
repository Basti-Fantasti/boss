package auth

// tryHostConfig implements layer 4: per-host protocol config from
// ~/.bossy/config.json. The cfg map is host → "ssh"|"https" pairs as
// stored by `bossy config git protocol`.
func tryHostConfig(parsed ParsedURL, cfg map[string]string) (Decision, bool) {
	if parsed.Host == "" || cfg == nil {
		return Decision{}, false
	}
	proto, ok := cfg[parsed.Host]
	if !ok {
		return Decision{}, false
	}
	switch proto {
	case "ssh":
		return Decision{
			URL:       "git@" + parsed.Host + ":" + parsed.Path,
			Transport: TransportSSH,
			Layer:     "host-config",
		}, true
	case "https":
		return Decision{
			URL:       "https://" + parsed.Host + "/" + parsed.Path,
			Transport: TransportHTTPS,
			Layer:     "host-config",
		}, true
	default:
		return Decision{}, false
	}
}
