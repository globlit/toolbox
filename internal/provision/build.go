// Package provision is operator-only build tooling. It is not linked into the
// gateway and cannot be invoked through either runtime transport.
package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globlit/toolbox/internal/config"
)

func Build(ctx context.Context, sourceRoot, configPath, output string) error {
	if !filepath.IsAbs(configPath) || !filepath.IsAbs(output) {
		return errors.New("config and output paths must be absolute")
	}
	b, err := config.ReadFile(configPath)
	if err != nil {
		return err
	}
	var c config.Config
	if config.Decode(b, &c) != nil || c.Validate() != nil {
		return errors.New("invalid operator configuration")
	}
	for _, protected := range []string{configPath, c.Kubectl.Binary, c.Kubectl.Kubeconfig} {
		p, _ := os.Stat(protected)
		o, _ := os.Stat(output)
		if filepath.Clean(output) == filepath.Clean(protected) || (p != nil && o != nil && os.SameFile(p, o)) {
			return errors.New("output cannot overwrite configuration, credentials or kubectl")
		}
	}
	// Quote linker assignments without shell interpolation. Go's -ldflags parser
	// accepts quoted fields but not escaped quotes; reject that rare path spelling.
	if strings.ContainsAny(configPath, "\"'\r\n") {
		return errors.New("configuration path cannot contain quotes or newlines")
	}
	f, err := os.Open(c.Kubectl.Binary)
	if err != nil {
		return errors.New("cannot read the approved kubectl executable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("kubectl must be a regular file")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.New("cannot hash the approved kubectl executable")
	}
	digest := sha256.Sum256(b)
	flags := `-s -w -X "github.com/globlit/toolbox/internal/config.pinnedPath=` + configPath + `" -X github.com/globlit/toolbox/internal/config.pinnedSHA256=` + hex.EncodeToString(digest[:]) + ` -X github.com/globlit/toolbox/internal/config.pinnedBinarySHA256=` + hex.EncodeToString(h.Sum(nil))
	cmd := exec.CommandContext(ctx, "go", "build", "-p=2", "-trimpath", "-ldflags="+flags, "-o", output, "./cmd/toolbox")
	cmd.Dir = sourceRoot
	// Operator build environment; none of it selects runtime trust anchors.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := cmd.Run(); err != nil {
		return errors.New("provisioned build failed")
	}
	return nil
}
