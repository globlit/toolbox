package runner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"toolbox/internal/testutil"
)

func TestD1ExecutableReplacementRejectedBeforeExecution(t *testing.T) {
	exe, _ := os.Executable()
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "kubectl")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, b, 0700); err != nil {
		t.Fatal(err)
	}
	r, err := New(path, testutil.BinaryHash(t, path), func(string) ([]string, []string, error) { return nil, nil, nil }, time.Second, 4096)
	if err != nil {
		t.Fatal(err)
	}
	// Keep its valid native header; a digest check must detect changed content.
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(path, b, 0700); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), []string{"inspect"})
	if err == nil || len(out.Stdout) > 0 || len(out.Stderr) > 0 {
		t.Fatal("changed executable ran", err)
	}
}

func TestD1ScriptsAreNotNativeExecutables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubectl")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	b := []byte("#!/bin/sh\nprintf 'should never execute'\n")
	_ = os.WriteFile(path, b, 0700)
	_, err := New(path, fmt.Sprintf("%x", sha256.Sum256(b)), func(string) ([]string, []string, error) { return nil, nil, nil }, time.Second, 4096)
	if err == nil {
		t.Fatal("script accepted as native executable")
	}
}
