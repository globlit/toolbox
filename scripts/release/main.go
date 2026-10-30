// Print the next stable release tag without modifying Git.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/globlit/toolbox/internal/release"
)

func main() {
	tags, err := exec.Command("git", "tag", "--list").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot list release tags")
		os.Exit(1)
	}
	current, err := exec.Command("git", "tag", "--points-at", "HEAD").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot list tags for HEAD")
		os.Exit(1)
	}
	next, err := release.Next(strings.Fields(string(tags)), strings.Fields(string(current)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(next)
}
