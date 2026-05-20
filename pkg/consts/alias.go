package consts

import "regexp"

// AliasNamePattern restricts alias names so they cannot collide with
// host names (which may contain dots) or the "git@" SSH prefix.
// Reserved names like "git", "http", "https", "ssh" match this pattern
// but are refused by the config command itself.
var AliasNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
