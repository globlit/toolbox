// Package release selects stable tags for automated releases.
package release

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
)

const FirstVersion = "v0.4.0"

var stable = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type number [3]int64

func parse(tag string) (number, bool) {
	parts := stable.FindStringSubmatch(tag)
	if parts == nil {
		return number{}, false
	}
	var n number
	for i := range n {
		value, err := strconv.ParseInt(parts[i+1], 10, 64)
		if err != nil {
			return number{}, false
		}
		n[i] = value
	}
	return n, true
}
func newer(a, b number) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// Next reuses an existing stable tag on the commit or increments the highest patch.
func Next(tags, onCommit []string) (string, error) {
	floor, _ := parse(FirstVersion)
	var current number
	found := false
	for _, tag := range onCommit {
		if n, ok := parse(tag); ok && (!found || newer(n, current)) {
			current, found = n, true
		}
	}
	if !found {
		var highest number
		for _, tag := range tags {
			if n, ok := parse(tag); ok && newer(n, highest) {
				highest = n
			}
		}
		if highest[2] == math.MaxInt64 {
			return "", errors.New("release patch number overflow")
		}
		highest[2]++
		current = floor
		if newer(highest, current) {
			current = highest
		}
	}
	if current[0] > 1 {
		return "", errors.New("major versions above v1 require a versioned Go module path")
	}
	return fmt.Sprintf("v%d.%d.%d", current[0], current[1], current[2]), nil
}

func Valid(tag string) bool { n, ok := parse(tag); return ok && n[0] <= 1 }
