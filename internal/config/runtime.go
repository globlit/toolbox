package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// LoadRuntime uses explicit caller-trusted configuration or the operator default.
// Environment variables never select configuration.
func LoadRuntime(path string) (Config, error) {
	if path == "" {
		return LoadOperator()
	}
	c, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	f, err := os.Open(c.Kubectl.Binary)
	if err != nil {
		return Config{}, errors.New("cannot read configured kubectl executable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Config{}, errors.New("kubectl must be a regular file")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Config{}, errors.New("cannot hash configured kubectl executable")
	}
	c.Kubectl.ExecutableSHA256 = hex.EncodeToString(h.Sum(nil))
	return c, nil
}
