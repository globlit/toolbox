// Package testutil provides an isolated fake kubectl process for transport tests.
// Production packages must never import this package.
package testutil

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"toolbox/internal/config"
)

func Hook() {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[1], "--kubeconfig=") {
		return
	}
	args := os.Args[1:]
	switch {
	case slices.Contains(args, "overflow"):
		fmt.Print(strings.Repeat("secretmaterial", 10000))
	case slices.Contains(args, "sleep"):
		time.Sleep(30 * time.Second)
	case slices.Contains(args, "inspect"):
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": args, "environment": os.Environ(), "cwd": cwd})
	case slices.ContainsFunc(args, func(a string) bool { return a == "failure" || strings.HasSuffix(a, "/failure") }):
		fmt.Fprintln(os.Stderr, "password=stderr-canary with spaces")
		fmt.Print("partial-sensitive-stdout")
		os.Exit(1)
	case slices.ContainsFunc(args, func(a string) bool { return a == "malformed" || strings.HasSuffix(a, "/malformed") }):
		fmt.Print(`{"password":"malformed-canary`)
	case slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, "/log?") }):
		fmt.Println(`{"level":"info","message":"ready","password":"log-canary","record":{"query":"SELECT 'customer-canary'"}}`)
	case slices.Contains(args, "get"):
		fmt.Print(`{"kind":"PodList","items":[{"metadata":{"name":"demo","annotations":{"note":"annotation-canary"}},"spec":{"containers":[{"name":"app","env":[{"name":"NORMAL","value":"env-canary"}]}]},"status":{"phase":"Running"}}]}`)
	default:
		os.Exit(9)
	}
	os.Exit(0)
}

func Config(t testing.TB) config.Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "kubeconfig.json")
	b := []byte(`{"apiVersion":"v1","kind":"Config","current-context":"reader","clusters":[{"name":"cluster","cluster":{"server":"https://127.0.0.1:1"}}],"users":[{"name":"reader","user":{"token":"credential-canary"}}],"contexts":[{"name":"reader","context":{"cluster":"cluster","user":"reader"}}]}`)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{Kubectl: config.Kubectl{Binary: exe, ExecutableSHA256: BinaryHash(t, exe), Kubeconfig: path, Context: "reader", DefaultNamespace: "apps", Namespaces: []string{"apps", "observability"}, TimeoutSeconds: 5, MaxOutputBytes: 65536}}
	return c
}

func BinaryHash(t testing.TB, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
