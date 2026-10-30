package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	c := Config{Kubectl: Kubectl{Binary: filepath.Join(t.TempDir(), "kubectl"), Kubeconfig: filepath.Join(t.TempDir(), "kubeconfig"), Context: "reader", DefaultNamespace: "apps", Namespaces: []string{"apps"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Kubectl.TimeoutSeconds != 30 || c.Kubectl.MaxOutputBytes != 1<<20 {
		t.Fatal(c)
	}
	if c.Kubectl.AllowPortForward || c.Kubectl.PortForwardMaxSeconds != 300 {
		t.Fatal("unsafe tunnel defaults", c)
	}
	for _, limit := range []int{-1, 3601} {
		bad := c
		bad.Kubectl.PortForwardMaxSeconds = limit
		if bad.Validate() == nil {
			t.Fatal("invalid tunnel limit accepted", limit)
		}
	}
	b, _ := json.Marshal(c)
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, b, 0600)
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{[]byte(`{"kubectl":{},"allow_writes":true}`), []byte(`{} {}`), []byte(`{"kubectl":{"namespaces":["*"]}}`)} {
		_ = os.WriteFile(path, b, 0600)
		if _, err := Load(path); err == nil {
			t.Fatal("unsafe config accepted")
		}
	}
}
