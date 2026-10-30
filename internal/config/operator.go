package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"path/filepath"
)

// Operator builds supply the default configuration path and digests.
// Explicit --config selects caller-trusted configuration instead.
var pinnedPath string
var pinnedSHA256 string
var pinnedBinarySHA256 string

func LoadOperator() (Config, error) { return loadPinned(pinnedPath, pinnedSHA256, pinnedBinarySHA256) }

func loadPinned(path, digest, binaryDigest string) (Config, error) {
	var c Config
	want, err := hex.DecodeString(digest)
	binaryHash, binaryErr := hex.DecodeString(binaryDigest)
	if !filepath.IsAbs(path) || err != nil || len(want) != sha256.Size || binaryErr != nil || len(binaryHash) != sha256.Size {
		return c, errors.New("configuration required. Use --config FILE or run scripts/provision")
	}
	b, err := ReadFile(path)
	if err != nil {
		return c, err
	}
	got := sha256.Sum256(b)
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return c, errors.New("operator configuration integrity check failed")
	}
	// Parse exactly the bytes whose digest was checked, never reread the path.
	if Decode(b, &c) != nil {
		return c, errors.New("invalid approved operator configuration")
	}
	c.Kubectl.ExecutableSHA256 = binaryDigest
	return c, c.Validate()
}
