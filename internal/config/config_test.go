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

func TestContextConfiguration(t *testing.T) {
	base := Config{Kubectl: Kubectl{Binary: filepath.Join(t.TempDir(), "kubectl"), Kubeconfig: filepath.Join(t.TempDir(), "kubeconfig"), Contexts: []Context{
		{Name: "prod-a", DefaultNamespace: "apps", Namespaces: []string{"apps"}},
		{Name: "prod-b", DefaultNamespace: "billing", Namespaces: []string{"billing"}, AllowPortForward: true},
	}}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	scopes := base.Kubectl.Scopes()
	if scopes[0].AllowPortForward || !scopes[1].AllowPortForward || scopes[1].Context != "prod-b" || scopes[1].TimeoutSeconds != 30 {
		t.Fatal(scopes)
	}
	for _, mutate := range []func(*Kubectl){
		func(k *Kubectl) { k.Context = "prod-a" },
		func(k *Kubectl) { k.Namespaces = []string{"apps"} },
		func(k *Kubectl) { k.DefaultNamespace = "apps" },
		func(k *Kubectl) { k.AllowClusterScoped = true },
		func(k *Kubectl) { k.AllowPortForward = true },
		func(k *Kubectl) { k.Contexts[1].Name = "prod-a" },
		func(k *Kubectl) { k.Contexts[1].Name = "" },
		func(k *Kubectl) { k.Contexts[1].Name = "prod\na" },
		func(k *Kubectl) { k.Contexts[1].Namespaces = []string{"prod-*"} },
		func(k *Kubectl) { k.Contexts[1].Namespaces = []string{"billing", "billing"} },
		func(k *Kubectl) { k.Contexts[1].DefaultNamespace = "apps" },
		func(k *Kubectl) { k.Contexts = make([]Context, 101) },
	} {
		c := base
		c.Kubectl.Contexts = append([]Context{}, base.Kubectl.Contexts...)
		mutate(&c.Kubectl)
		if c.Validate() == nil {
			t.Fatal("invalid context configuration accepted", c.Kubectl)
		}
	}
}

func TestNamespaceSelections(t *testing.T) {
	for _, selection := range [][]string{{"*"}, {"*", "apps"}, {"apps"}} {
		c := Config{Kubectl: Kubectl{Binary: filepath.Join(t.TempDir(), "kubectl"), Kubeconfig: filepath.Join(t.TempDir(), "config"), Context: "reader", DefaultNamespace: "apps", Namespaces: selection, ConfigMapNamespaces: []string{"*"}, DisabledConfigMapNamespaces: []string{"private"}}}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, bad := range [][]string{{"prod-*"}, {"*", "*"}, {"bad/name"}} {
			invalid := c
			invalid.Kubectl.ConfigMapNamespaces = bad
			if invalid.Validate() == nil {
				t.Fatal("invalid selection accepted", bad)
			}
			invalid = c
			invalid.Kubectl.DisabledConfigMapNamespaces = bad
			if invalid.Validate() == nil {
				t.Fatal("invalid exclusion accepted", bad)
			}
		}
	}
}
