// Package runner executes only plans produced inside trusted integrations.
// It has no CLI or MCP exposure of its own.
package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Result struct{ Stdout, Stderr []byte }

// Setup is integration-owned code, never an agent-controlled callback.
// It creates credential files in the private directory and returns fixed options.
type Setup func(dir string) (prefix []string, environment []string, err error)
type Runner struct {
	binary  string
	digest  []byte
	setup   Setup
	timeout time.Duration
	limit   int
	slots   chan struct{}
}

func New(binary, digest string, setup Setup, timeout time.Duration, limit int) (*Runner, error) {
	if !filepath.IsAbs(binary) {
		return nil, errors.New("tool binary must be an absolute path")
	}
	path, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return nil, errors.New("tool executable is unavailable")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) || (runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe")) {
		return nil, errors.New("tool must be a native executable")
	}
	if setup == nil || timeout <= 0 || limit <= 0 {
		return nil, errors.New("invalid execution configuration")
	}
	expected, err := hex.DecodeString(digest)
	if err != nil || len(expected) != sha256.Size {
		return nil, errors.New("tool executable digest is missing or invalid")
	}
	r := &Runner{binary: path, digest: expected, setup: setup, timeout: timeout, limit: limit, slots: make(chan struct{}, 2)}
	if err := r.verifyExecutable(); err != nil {
		return nil, err
	}
	return r, nil
}

type capture struct {
	mu          sync.Mutex
	used, limit int
	overflow    bool
	cancel      context.CancelFunc
}
type stream struct {
	c *capture
	b bytes.Buffer
}

func (s *stream) Write(p []byte) (int, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.overflow {
		return len(p), nil
	}
	if len(p) > s.c.limit-s.c.used {
		s.c.overflow = true
		s.c.cancel()
		return len(p), nil
	}
	s.c.used += len(p)
	return s.b.Write(p)
}

func (r *Runner) Run(parent context.Context, args []string) (Result, error) {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		return Result{}, errors.New("toolbox is busy. Retry later")
	}
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "toolbox-")
	if err != nil {
		return Result{}, errors.New("cannot create private execution directory")
	}
	defer os.RemoveAll(dir)
	prefix, environment, err := r.setup(dir)
	if err != nil {
		return Result{}, errors.New("cannot prepare isolated tool environment")
	}
	argv := append(prefix, args...)
	cmd := exec.CommandContext(ctx, r.binary, argv...)
	cmd.Dir = dir
	// Do not inherit KUBECONFIG, proxy, loader, cloud, debugging or plugin variables.
	cmd.Env = []string{"HOME=" + dir, "USERPROFILE=" + dir, "TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir, "PATH=", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "NO_COLOR=1"}
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	cmd.Env = append(cmd.Env, environment...)
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	c := &capture{limit: r.limit, cancel: cancel}
	out := &stream{c: c}
	stderr := &stream{c: c}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := r.verifyExecutable(); err != nil {
		return Result{}, err
	}
	err = cmd.Run()
	if c.overflow {
		return Result{}, errors.New("output exceeded the limit. Response discarded. Narrow the query or reduce tail")
	}
	if ctx.Err() != nil {
		return Result{}, errors.New("request cancelled or timed out. Partial output discarded")
	}
	result := Result{out.b.Bytes(), stderr.b.Bytes()}
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return result, errors.New("tool request failed")
		}
		return Result{}, errors.New("tool execution failed")
	}
	return result, nil
}

// File permissions must also prevent replacement between verification and exec.
// The digest blocks ordinary replacement, not a concurrent same-user filesystem attacker.
func (r *Runner) verifyExecutable() error {
	f, err := os.Open(r.binary)
	if err != nil {
		return errors.New("tool executable integrity check failed")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return errors.New("tool executable integrity check failed")
	}
	var header [4]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return errors.New("tool must be a native executable")
	}
	native := false
	switch runtime.GOOS {
	case "linux":
		native = header == [4]byte{0x7f, 'E', 'L', 'F'}
	case "windows":
		native = header[0] == 'M' && header[1] == 'Z'
	case "darwin":
		switch header {
		case [4]byte{0xfe, 0xed, 0xfa, 0xce}, [4]byte{0xce, 0xfa, 0xed, 0xfe}, [4]byte{0xfe, 0xed, 0xfa, 0xcf}, [4]byte{0xcf, 0xfa, 0xed, 0xfe}, [4]byte{0xca, 0xfe, 0xba, 0xbe}, [4]byte{0xbe, 0xba, 0xfe, 0xca}, [4]byte{0xca, 0xfe, 0xba, 0xbf}, [4]byte{0xbf, 0xba, 0xfe, 0xca}:
			native = true
		}
	}
	if !native {
		return errors.New("tool must be a native executable")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return errors.New("tool executable integrity check failed")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil || subtle.ConstantTimeCompare(h.Sum(nil), r.digest) != 1 {
		return errors.New("tool executable integrity check failed")
	}
	return nil
}
