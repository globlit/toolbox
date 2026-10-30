package version

import (
	"runtime/debug"
	"testing"
)

func TestVersionSources(t *testing.T) {
	for _, tc := range []struct{ override, module, want string }{
		{"v0.4.0", "(devel)", "v0.4.0"}, {"", "v0.4.1", "v0.4.1"},
		{"", "v0.4.2-0.20261030120000-abcdef123456", "v0.4.2-0.20261030120000-abcdef123456"},
		{"", "(devel)", "dev"}, {"", "", "dev"},
	} {
		info := &debug.BuildInfo{Main: debug.Module{Version: tc.module}}
		if got := resolve(tc.override, info); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	if resolve("", nil) != "dev" {
		t.Fatal("missing metadata did not use dev")
	}
}
