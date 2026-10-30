package testutil

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/globlit/toolbox/internal/config"
	"github.com/globlit/toolbox/internal/provision"
)

func BuildGateway(t testing.TB, c config.Config) (binary, configPath string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "operator.json")
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	binary = filepath.Join(dir, "toolbox")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("module root not found")
		}
		root = parent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := provision.Build(ctx, root, configPath, binary); err != nil {
		t.Fatal(err)
	}
	return binary, configPath
}
