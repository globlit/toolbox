package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"toolbox/internal/config"
	"toolbox/internal/testutil"
)

func TestRuntimeConfigLoadsExplicitFile(t *testing.T) {
	c := testutil.MultiContextConfig(t)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "caller config.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOOLBOX_CONFIG", filepath.Join(dir, "missing"))
	t.Setenv("KUBECONFIG", filepath.Join(dir, "missing"))
	got, err := config.LoadRuntime(path)
	if err != nil || len(got.Kubectl.Contexts) != 2 || got.Kubectl.ExecutableSHA256 != testutil.BinaryHash(t, c.Kubectl.Binary) {
		t.Fatal(got, err)
	}
	t.Chdir(dir)
	if _, err = config.LoadRuntime("caller config.json"); err != nil {
		t.Fatal(err)
	}
	c.Kubectl.Contexts = c.Kubectl.Contexts[:1]
	b, _ = json.Marshal(c)
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = config.LoadRuntime(path)
	if err != nil || len(got.Kubectl.Contexts) != 1 {
		t.Fatal("explicit config update rejected", got, err)
	}
}

func TestRuntimeConfigFailsClosed(t *testing.T) {
	c := testutil.Config(t)
	path := filepath.Join(t.TempDir(), "config.json")
	for _, content := range []string{`{"allow_writes":true}`, `{"kubectl":{"binary":"relative"}}`, `{} {}`} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadRuntime(path); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	for _, binary := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		c.Kubectl.Binary = binary
		b, _ := json.Marshal(c)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadRuntime(path); err == nil {
			t.Fatal("invalid executable accepted")
		}
	}
	if _, err := config.LoadRuntime(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}
