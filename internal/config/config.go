// Package config loads operator-owned configuration. Agent requests never change it.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const MaxFileBytes = 1 << 20

type Config struct {
	Kubectl   Kubectl   `json:"kubectl"`
	Redaction Redaction `json:"redaction"`
}

type Kubectl struct {
	// Supplied by the trusted build, never deserialized from operator/request JSON.
	ExecutableSHA256      string   `json:"-"`
	Binary                string   `json:"binary"`
	Kubeconfig            string   `json:"kubeconfig"`
	Context               string   `json:"context"`
	DefaultNamespace      string   `json:"default_namespace"`
	Namespaces            []string `json:"namespaces"`
	AllowClusterScoped    bool     `json:"allow_cluster_scoped"`
	TimeoutSeconds        int      `json:"timeout_seconds"`
	MaxOutputBytes        int      `json:"max_output_bytes"`
	AllowPortForward      bool     `json:"allow_port_forward"`
	PortForwardMaxSeconds int      `json:"port_forward_max_seconds"`
}

type Redaction struct {
	Literals []string `json:"literals"`
	Patterns []string `json:"patterns"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func Namespace(s string) bool { return len(s) <= 63 && dnsLabel.MatchString(s) }

func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open operator configuration or credential file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("configuration must be a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil || len(b) > MaxFileBytes {
		return nil, errors.New("cannot read configuration within the 1 MiB limit")
	}
	return b, nil
}

func Load(path string) (Config, error) {
	var c Config
	f, err := ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := Decode(f, &c); err != nil {
		return c, errors.New("invalid configuration: unknown field or invalid JSON")
	}
	return c, c.Validate()
}

func Decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func (c *Config) Validate() error {
	k := &c.Kubectl
	if !filepath.IsAbs(k.Binary) || !filepath.IsAbs(k.Kubeconfig) || k.Context == "" || len(k.Context) > 253 {
		return errors.New("require absolute binary and kubeconfig paths and a context of 1..253 bytes")
	}
	if len(k.Namespaces) == 0 || len(k.Namespaces) > 100 || !Namespace(k.DefaultNamespace) {
		return errors.New("require 1..100 namespaces and a valid default_namespace")
	}
	found := false
	for _, ns := range k.Namespaces {
		if !Namespace(ns) {
			return errors.New("invalid namespace in configuration")
		}
		if ns == k.DefaultNamespace {
			found = true
		}
	}
	if !found {
		return errors.New("default_namespace must be in namespaces")
	}
	if k.TimeoutSeconds == 0 {
		k.TimeoutSeconds = 30
	}
	if k.PortForwardMaxSeconds == 0 {
		k.PortForwardMaxSeconds = 300
	}
	if k.PortForwardMaxSeconds < 1 || k.PortForwardMaxSeconds > 3600 {
		return errors.New("port_forward_max_seconds must be from 1 to 3600")
	}
	if k.MaxOutputBytes == 0 {
		k.MaxOutputBytes = 1 << 20
	}
	if k.TimeoutSeconds < 1 || k.TimeoutSeconds > 120 || k.MaxOutputBytes < 4096 || k.MaxOutputBytes > 8<<20 {
		return errors.New("configuration limits: timeout 1..120 seconds, output 4096..8388608 bytes")
	}
	if len(c.Redaction.Literals) > 256 || len(c.Redaction.Patterns) > 64 {
		return errors.New("redaction limits: 256 literals, 64 patterns")
	}
	for _, s := range c.Redaction.Literals {
		if len(s) < 3 || len(s) > 4096 {
			return errors.New("redaction literals must contain 3..4096 bytes")
		}
	}
	for _, s := range c.Redaction.Patterns {
		if len(s) == 0 || len(s) > 4096 {
			return errors.New("redaction patterns must contain 1..4096 bytes")
		}
		if _, err := regexp.Compile(s); err != nil {
			return errors.New("invalid redaction regular expression")
		}
	}
	return nil
}
