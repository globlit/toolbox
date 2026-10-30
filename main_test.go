package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestVersionedInstall exercises Go's module installer against a local file proxy.
func TestVersionedInstall(t *testing.T) {
	const module = "github.com/globlit/toolbox"
	const tag = "v0.4.0"
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(t.TempDir(), "proxy")
	versions := filepath.Join(proxy, filepath.FromSlash(module), "@v")
	if err = os.MkdirAll(versions, 0755); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if !(strings.HasSuffix(relative, ".go") || relative == "go.mod" || relative == "go.sum" || relative == "LICENSE") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dest, err := z.Create(module + "@" + tag + "/" + filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		_, err = dest.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(map[string]string{"Version": tag, "Time": "2026-10-30T00:00:00Z"})
	for name, data := range map[string][]byte{tag + ".zip": archive.Bytes(), tag + ".mod": mod, tag + ".info": info, "list": []byte(tag + "\n")} {
		if err = os.WriteFile(filepath.Join(versions, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(t.TempDir(), "bin")
	// file:/// works for POSIX paths and Windows drive paths.
	proxyURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(proxy), "/")
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	dependencyProxy := "file:///" + strings.TrimPrefix(filepath.ToSlash(filepath.Join(strings.TrimSpace(string(cache)), "cache", "download")), "/")
	moduleCache := filepath.Join(t.TempDir(), "modules")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, query := range []string{tag, "latest"} {
		cmd := exec.CommandContext(ctx, "go", "install", module+"@"+query)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "GOBIN="+bin, "GOPROXY="+proxyURL+","+dependencyProxy+",https://proxy.golang.org", "GOMODCACHE="+moduleCache, "GONOSUMDB="+module, "GOPRIVATE=", "GONOPROXY=", "GOWORK=off", "GOFLAGS=-modcacherw", "GOMAXPROCS=2")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(query, string(out), err)
		}
		executable := filepath.Join(bin, "toolbox")
		if runtime.GOOS == "windows" {
			executable += ".exe"
		}
		out, err := exec.CommandContext(ctx, executable, "version").CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "toolbox "+tag {
			t.Fatal(query, string(out), err)
		}
	}
}
