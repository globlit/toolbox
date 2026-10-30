// Package version reports release or Go module build metadata.
package version

import "runtime/debug"

var release string

func Current() string {
	info, _ := debug.ReadBuildInfo()
	return resolve(release, info)
}

func resolve(override string, info *debug.BuildInfo) string {
	if override != "" {
		return override
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
