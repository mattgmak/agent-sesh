package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("AGENT_SESH_AGENTS", "")
	t.Setenv("AGENT_SESH_CONFIG", filepath.Join(t.TempDir(), "missing.json"))

	if got := Load().Agents; len(got) != 1 || got[0] != DefaultAgent {
		t.Fatalf("Load() agents = %v, want [%s]", got, DefaultAgent)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"agents":["pi","pi-local","pi-local","pi-stock",""]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_SESH_AGENTS", "")
	t.Setenv("AGENT_SESH_CONFIG", path)

	if got, want := Load().Agents, []string{"pi", "pi-local", "pi-stock"}; !equal(got, want) {
		t.Fatalf("Load() agents = %v, want %v", got, want)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"agents":["pi"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_SESH_CONFIG", path)
	t.Setenv("AGENT_SESH_AGENTS", " pi-local , pi-stock ,,")

	if got, want := Load().Agents, []string{"pi-local", "pi-stock"}; !equal(got, want) {
		t.Fatalf("Load() agents = %v, want %v", got, want)
	}
}

func TestAgentsCachesAndIsAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("AGENT_SESH_AGENTS", "")
	t.Setenv("AGENT_SESH_CONFIG", path)

	if err := os.WriteFile(path, []byte(`{"version":1,"agents":["pi-local"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := Agents(), []string{"pi-local"}; !equal(got, want) {
		t.Fatalf("Agents() = %v, want %v", got, want)
	}

	// The cached answer survives a file rewrite inside the TTL window.
	if err := os.WriteFile(path, []byte(`{"version":1,"agents":["pi-stock"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := Agents(), []string{"pi-local"}; !equal(got, want) {
		t.Fatalf("Agents() = %v after rewrite, want cached %v", got, want)
	}

	if !IsAgent("pi-local") || !IsAgent("  pi-local ") {
		t.Fatal("IsAgent() = false for a configured name")
	}
	if IsAgent("pi") || IsAgent("codex") {
		t.Fatal("IsAgent() = true for an unconfigured name")
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
