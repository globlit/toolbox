package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestD1PinnedConfigRejectsReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.json")
	c := Config{Kubectl: Kubectl{Binary: filepath.Join(dir, "kubectl"), Kubeconfig: filepath.Join(dir, "credentials"), Context: "reader", DefaultNamespace: "apps", Namespaces: []string{"apps"}}}
	b, _ := json.Marshal(c)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(b))
	binaryDigest := strings.Repeat("a", 64)
	got, err := loadPinned(path, digest, binaryDigest)
	if err != nil || got.Kubectl.ExecutableSHA256 != binaryDigest {
		t.Fatal(got, err)
	}
	c.Kubectl.Binary = filepath.Join(dir, "attacker")
	changed, _ := json.Marshal(c)
	_ = os.WriteFile(path, changed, 0600)
	if _, err := loadPinned(path, digest, binaryDigest); err == nil {
		t.Fatal("changed config accepted")
	}
	for _, v := range [][3]string{{path, "", binaryDigest}, {path, digest, ""}, {"relative.json", digest, binaryDigest}, {path, "not-a-digest", binaryDigest}} {
		if _, err := loadPinned(v[0], v[1], v[2]); err == nil {
			t.Fatal("missing trust anchor accepted")
		}
	}
}
