package release

import "testing"

func TestNextVersion(t *testing.T) {
	for _, tc := range []struct {
		tags, current []string
		want          string
	}{
		{nil, nil, "v0.4.0"},
		{[]string{"v0.3.0"}, nil, "v0.4.0"},
		{[]string{"v0.4.9", "v0.4.10"}, nil, "v0.4.11"},
		{[]string{"v0.4.10", "v0.4.0"}, []string{"v0.4.0"}, "v0.4.0"},
		{[]string{"v0.9.99", "v1.0.0"}, nil, "v1.0.1"},
		{[]string{"latest", "v01.0.0", "v0.5.0-rc.1", "v0.4.0"}, nil, "v0.4.1"},
	} {
		got, err := Next(tc.tags, tc.current)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	for _, tags := range [][]string{{"v2.0.0"}, {"v1.0.9223372036854775807"}} {
		if _, err := Next(tags, nil); err == nil {
			t.Fatal("unsupported version accepted", tags)
		}
	}
	for _, tag := range []string{"dev", "v0.4.0; echo bad", "v2.0.0", "v0.4.0-rc.1", "v01.0.0"} {
		if Valid(tag) {
			t.Fatal("invalid release version accepted", tag)
		}
	}
}
