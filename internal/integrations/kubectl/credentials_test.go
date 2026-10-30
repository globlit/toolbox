package kubectl

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"toolbox/internal/testutil"
)

func TestCredentialSnapshot(t *testing.T) {
	c := testutil.Config(t)
	snapshot, literals, err := credentialSnapshot(c.Kubectl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snapshot), `"current-context":"toolbox"`) || len(literals) != 1 {
		t.Fatal("snapshot not rebuilt")
	}
	if err := os.WriteFile(c.Kubectl.Kubeconfig, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(snapshot) {
		t.Fatal("snapshot changed with source")
	}
}

func TestRejectDangerousKubeconfig(t *testing.T) {
	for _, field := range []string{`"exec":{"command":"sh","args":["-c","touch sentinel"]}`, `"auth-provider":{"name":"gcp"}`, `"as":"admin"`, `"tokenFile":"/etc/passwd"`, `"unknown":"value"`} {
		t.Run(field, func(t *testing.T) {
			c := testutil.Config(t)
			b, _ := os.ReadFile(c.Kubectl.Kubeconfig)
			b = []byte(strings.Replace(string(b), `"token":"credential-canary"`, `"token":"credential-canary",`+field, 1))
			_ = os.WriteFile(c.Kubectl.Kubeconfig, b, 0600)
			if _, _, err := credentialSnapshot(c.Kubectl); err == nil {
				t.Fatal("unsafe credentials accepted")
			}
		})
	}
	for _, change := range []struct{ old, new string }{{`"server":"https://127.0.0.1:1"`, `"server":"http://127.0.0.1:1"`}, {`"server":"https://127.0.0.1:1"`, `"server":"https://user:pass@host"`}, {`"server":"https://127.0.0.1:1"`, `"server":"https://host/path"`}, {`"server":"https://127.0.0.1:1"`, `"server":"https://host","proxy-url":"https://proxy"`}, {`"server":"https://127.0.0.1:1"`, `"server":"https://host","insecure-skip-tls-verify":true`}} {
		c := testutil.Config(t)
		b, _ := os.ReadFile(c.Kubectl.Kubeconfig)
		_ = os.WriteFile(c.Kubectl.Kubeconfig, []byte(strings.Replace(string(b), change.old, change.new, 1)), 0600)
		if _, _, err := credentialSnapshot(c.Kubectl); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}
