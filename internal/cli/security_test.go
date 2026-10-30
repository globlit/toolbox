package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"toolbox/internal/config"
)

func TestD1RuntimeCannotChooseConfigOrExecutable(t *testing.T) {
	for _, args := range [][]string{
		{"--config", "attacker.json", "kubectl", "get", "pods"},
		{"--config=attacker.json", "mcp"},
		{"mcp", "--config", "attacker.json"},
		{"kubectl", "get", "pods", "--config=attacker.json"},
		{"kubectl", "get", "pods", "--binary=/tmp/attacker"},
		{"provision", "--config", "attacker.json"},
	} {
		calls := 0
		var out, stderr bytes.Buffer
		code := run(context.Background(), args, &out, &stderr, func() (config.Config, error) { calls++; return config.Config{}, nil })
		if code != 2 || calls != 0 || out.Len() != 0 {
			t.Fatal(args, code, calls, out.String())
		}
	}
}

func TestD1UnprovisionedBuildHasNoEnvironmentFallback(t *testing.T) {
	t.Setenv("TOOLBOX_CONFIG", "attacker.json")
	t.Setenv("TOOLBOX_CONFIG_PATH", "attacker.json")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"kubectl", "get", "pods"}, &out, &stderr)
	if code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "operator build required") {
		t.Fatal(code, out.String(), stderr.String())
	}
}
