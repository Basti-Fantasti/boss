//nolint:testpackage // Tests internal helpers of the alias command
package config

import (
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/spf13/cobra"
)

// newTestConfig returns a *env.Configuration whose SaveConfiguration writes
// to a per-test temp directory so the user's real config is never touched.
func newTestConfig(t *testing.T) *env.Configuration {
	t.Helper()
	t.Setenv("BOSS_HOME", t.TempDir())
	cfg, _ := env.LoadConfiguration(env.GetBossHome())
	return cfg
}

func TestAliasCmd_Registration(t *testing.T) {
	configRoot := &cobra.Command{Use: "config"}
	configRoot.AddCommand(aliasCmd())

	var found *cobra.Command
	for _, c := range configRoot.Commands() {
		if strings.HasPrefix(c.Use, "alias") {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("alias subcommand not registered")
	}
	if found.Short == "" {
		t.Error("alias command should have a Short description")
	}
	if f := found.Flags().Lookup("list"); f == nil {
		t.Error("--list flag missing")
	}
	if f := found.Flags().Lookup("unset"); f == nil {
		t.Error("--unset flag missing")
	}
}

func TestAliasAction_SetValid(t *testing.T) {
	cfg := newTestConfig(t)

	out, err := aliasAction{args: []string{"gtr", "gitlab.example.com"}}.run(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Aliases["gtr"] != "gitlab.example.com" {
		t.Errorf("alias not stored, got %q", cfg.Aliases["gtr"])
	}
	if len(out) != 1 || !strings.Contains(out[0], "gtr") {
		t.Errorf("unexpected output: %#v", out)
	}
}

func TestAliasAction_SetInvalidName(t *testing.T) {
	cfg := newTestConfig(t)

	// Dot is forbidden by AliasNamePattern.
	_, err := aliasAction{args: []string{"bad.name", "host"}}.run(cfg)
	if err == nil {
		t.Fatal("expected error for invalid alias name")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected 'invalid' in error, got %q", err.Error())
	}
	if _, ok := cfg.Aliases["bad.name"]; ok {
		t.Error("invalid alias should not have been stored")
	}
}

func TestAliasAction_SetReservedName(t *testing.T) {
	cfg := newTestConfig(t)

	_, err := aliasAction{args: []string{"https", "example.com"}}.run(cfg)
	if err == nil {
		t.Fatal("expected error for reserved alias name")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("expected 'reserved' in error, got %q", err.Error())
	}
}

func TestAliasAction_SetCollidesWithHostProtocol(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.HostProtocols = map[string]string{"gtr": "ssh"}

	_, err := aliasAction{args: []string{"gtr", "gitlab.example.com"}}.run(cfg)
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "collides") {
		t.Errorf("expected 'collides' in error, got %q", err.Error())
	}
	if _, ok := cfg.Aliases["gtr"]; ok {
		t.Error("colliding alias should not have been stored")
	}
}

func TestAliasAction_Unset(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Aliases = map[string]string{"gtr": "gitlab.example.com"}

	out, err := aliasAction{unset: "gtr"}.run(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.Aliases["gtr"]; ok {
		t.Error("alias should have been removed")
	}
	if len(out) != 1 || !strings.Contains(out[0], "removed") {
		t.Errorf("unexpected output: %#v", out)
	}
}

func TestAliasAction_UnsetUnknown(t *testing.T) {
	cfg := newTestConfig(t)

	_, err := aliasAction{unset: "missing"}.run(cfg)
	if err == nil {
		t.Fatal("expected error for unsetting unknown alias")
	}
	if !strings.Contains(err.Error(), "not set") {
		t.Errorf("expected 'not set' in error, got %q", err.Error())
	}
}

func TestAliasAction_ListEmpty(t *testing.T) {
	cfg := newTestConfig(t)

	out, err := aliasAction{list: true}.run(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || !strings.Contains(out[0], "No aliases") {
		t.Errorf("expected empty-state message, got %#v", out)
	}
}

func TestAliasAction_ListPopulated(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Aliases = map[string]string{
		"gtr":  "gitlab.example.com",
		"acme": "git.acme.io",
	}

	out, err := aliasAction{list: true}.run(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 lines, got %d: %#v", len(out), out)
	}
	// Sorted alphabetically: "acme" before "gtr".
	if !strings.HasPrefix(out[0], "acme") {
		t.Errorf("expected acme first, got %q", out[0])
	}
	if !strings.HasPrefix(out[1], "gtr") {
		t.Errorf("expected gtr second, got %q", out[1])
	}
}
