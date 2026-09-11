package consts

import "regexp"

// AliasNamePattern restricts alias names so they cannot collide with
// host names (which may contain dots) or the "git@" SSH prefix.
// Reserved names like "git", "http", "https", "ssh" match this pattern
// but are refused by the config command itself.
var AliasNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// PresetIDPattern restricts preset catalog ids. Uppercase is allowed
// deliberately: the GTR library configuration carries idents such as
// PYTHONENVIRONMENTDIR, and forcing them to lowercase would make the catalog
// disagree with the source it is generated from. Slashes and spaces are
// refused so an id can never be confused with a repository key.
var PresetIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)
