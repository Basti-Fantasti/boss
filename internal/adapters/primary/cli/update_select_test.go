//nolint:testpackage // Drives the unexported option builder directly
package cli

import (
	"testing"
)

// TestSelectedUpdateOptions_ReResolves pins the two properties that made
// `bossy update --select` a no-op.
//
// The picker collects repository keys, so the options it builds must carry
// repository keys — the installer matches ForceUpdate against dep.Repository,
// and a short module name ("horse") is not that key and is ambiguous besides.
//
// And selecting a dependency is an update: LockedVersion must stay false so the
// selection re-resolves against the remote. Replaying the lock re-checks-out the
// SHA already recorded, which is precisely what "update" is asking not to do.
func TestSelectedUpdateOptions_ReResolves(t *testing.T) {
	selected := []string{"github.com/hashload/horse", "github.com/hashload/jhonson"}

	opts := selectedUpdateOptions(selected)

	if opts.LockedVersion {
		t.Error("LockedVersion is true: the selection would replay the lock instead of re-resolving")
	}

	assertSameKeys(t, "Args", opts.Args, selected)
	assertSameKeys(t, "ForceUpdate", opts.ForceUpdate, selected)
}

// assertSameKeys fails unless got holds exactly want, in order. The repository
// key shape is the whole point: a future switch to short names has to fail here
// rather than silently stop matching inside the installer.
func assertSameKeys(t *testing.T, field string, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want the repository key %q", field, i, got[i], want[i])
		}
	}
}
