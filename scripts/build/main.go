// Build portable release binaries sequentially to keep peak memory bounded.
package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globlit/toolbox/internal/release"
)

func main() {
	tag := flag.String("version", "", "Release version, such as v0.4.0. Omit for a development build.")
	flag.Parse()
	if err := build(*tag); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func build(tag string) error {
	if flag.NArg() != 0 || (tag != "" && !release.Valid(tag)) {
		return errors.New("require a stable v0 or v1 release version")
	}
	flags := "-s -w"
	if tag != "" {
		flags += " -X github.com/globlit/toolbox/internal/version.release=" + tag
	}
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	if tag == "" {
		tag = "dev"
	}
	if err := os.WriteFile(filepath.Join("dist", "VERSION"), []byte(tag+"\n"), 0644); err != nil {
		return err
	}
	var sums strings.Builder
	fmt.Fprintf(&sums, "%x  VERSION\n", sha256.Sum256([]byte(tag+"\n")))
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"} {
		parts := strings.Split(target, "/")
		name := "toolbox-" + parts[0] + "-" + parts[1]
		if parts[0] == "windows" {
			name += ".exe"
		}
		path := filepath.Join("dist", name)
		cmd := exec.Command("go", "build", "-p=2", "-trimpath", "-ldflags="+flags, "-o", path, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Println("Build", target)
		if err := cmd.Run(); err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(b), name)
	}
	return os.WriteFile(filepath.Join("dist", "SHA256SUMS"), []byte(sums.String()), 0644)
}
