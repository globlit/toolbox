// Build portable release binaries sequentially to keep peak memory bounded.
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func build() error {
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	var sums strings.Builder
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"} {
		parts := strings.Split(target, "/")
		name := "toolbox-" + parts[0] + "-" + parts[1]
		if parts[0] == "windows" {
			name += ".exe"
		}
		path := filepath.Join("dist", name)
		cmd := exec.Command("go", "build", "-p=2", "-trimpath", "-ldflags=-s -w", "-o", path, "./cmd/toolbox")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Println("Building", target)
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
