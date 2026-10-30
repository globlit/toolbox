package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"toolbox/internal/testutil"
)

func init() { testutil.Hook() }
func testRunner(t *testing.T, timeout time.Duration, limit int) *Runner {
	t.Helper()
	exe, _ := os.Executable()
	r, err := New(exe, testutil.BinaryHash(t, exe), func(dir string) ([]string, []string, error) {
		path := filepath.Join(dir, "snapshot")
		err := os.WriteFile(path, []byte("snapshot"), 0600)
		return []string{"--kubeconfig=" + path}, []string{"KUBECTL_KUBERC=false"}, err
	}, timeout, limit)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHermeticExecutionAndCleanup(t *testing.T) {
	t.Setenv("HTTP_PROXY", "proxy-canary")
	t.Setenv("KUBECONFIG", "config-canary")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "cloud-canary")
	r := testRunner(t, 5*time.Second, 65536)
	result, err := r.Run(context.Background(), []string{"inspect"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args        []string
		Environment []string
		Cwd         string
	}
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Environment {
		if strings.Contains(e, "canary") {
			t.Fatal("inherited sensitive environment")
		}
	}
	if _, err := os.Stat(got.Cwd); !os.IsNotExist(err) {
		t.Fatal("private directory not removed")
	}
	if !strings.Contains(strings.Join(got.Environment, " "), "KUBECTL_KUBERC=false") {
		t.Fatal("kuberc not disabled")
	}
	if !strings.HasPrefix(got.Args[0], "--kubeconfig=") {
		t.Fatal(got.Args)
	}
}

func TestOverflowAndTimeoutWithholdEverything(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
	}{{"overflow", 5 * time.Second}, {"sleep", 100 * time.Millisecond}} {
		t.Run(tc.name, func(t *testing.T) {
			r := testRunner(t, tc.timeout, 4096)
			start := time.Now()
			out, err := r.Run(context.Background(), []string{tc.name})
			if err == nil || len(out.Stdout) != 0 || len(out.Stderr) != 0 {
				t.Fatal("partial output escaped", err)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("termination was not bounded")
			}
		})
	}
}

func TestConcurrencyLimit(t *testing.T) {
	r := testRunner(t, time.Second, 4096)
	r.slots <- struct{}{}
	r.slots <- struct{}{}
	_, err := r.Run(context.Background(), []string{"inspect"})
	if err == nil {
		t.Fatal("concurrency cap ignored")
	}
}
