package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type githubReply struct {
	operation string
	output    string
	failure   bool
}

func TestGitHubReleaseRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release automation runs on Ubuntu")
	}
	for _, dependency := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(dependency); err != nil {
			t.Fatalf("release test requires %s: %v", dependency, err)
		}
	}
	missing := githubReply{"view", "release not found\n", true}
	draft := githubReply{"view", `{"isDraft":true}`, false}
	published := githubReply{"view", `{"isDraft":false}`, false}
	unavailable := githubReply{"view", "HTTP 503: Service Unavailable", true}
	createFailed := githubReply{"create", "HTTP 502: Server Error", true}
	for _, tc := range []struct {
		name    string
		command string
		replies []githubReply
		want    string
		fail    bool
		sleeps  int
	}{
		{"missing", "state", []githubReply{missing}, "missing\n", false, 0},
		{"draft", "state", []githubReply{draft}, "draft\n", false, 0},
		{"published", "state", []githubReply{published}, "published\n", false, 0},
		{"lookup outage", "state", []githubReply{unavailable, draft}, "draft\n", false, 1},
		{"lookup outage exhausted", "ensure-draft", []githubReply{unavailable, unavailable, unavailable, unavailable, unavailable}, "", true, 4},
		{"unauthorized", "ensure-draft", []githubReply{{"view", "HTTP 401: Unauthorized", true}}, "", true, 0},
		{"forbidden", "ensure-draft", []githubReply{{"view", "HTTP 403: Forbidden", true}}, "", true, 0},
		{"unclassified not found", "ensure-draft", []githubReply{{"view", "HTTP 404: Not Found", true}}, "", true, 0},
		{"invalid JSON", "state", []githubReply{{"view", "{", false}}, "", true, 0},
		{"invalid draft type", "state", []githubReply{{"view", `{"isDraft":"false"}`, false}}, "", true, 0},
		{"absent draft field", "state", []githubReply{{"view", `{}`, false}}, "", true, 0},
		{"create", "ensure-draft", []githubReply{missing, {"create", "release URL", false}}, "", false, 0},
		{"resume draft", "ensure-draft", []githubReply{draft}, "", false, 0},
		{"create succeeded despite gateway error", "ensure-draft", []githubReply{missing, createFailed, draft}, "", false, 1},
		{"create failed before reaching server", "ensure-draft", []githubReply{missing, createFailed, missing, {"create", "", false}}, "", false, 1},
		{"create outage exhausted", "ensure-draft", []githubReply{missing, createFailed, missing, createFailed, missing, createFailed, missing, createFailed, missing, createFailed, missing}, "", true, 4},
		{"create final response ambiguous", "ensure-draft", []githubReply{missing, createFailed, missing, createFailed, missing, createFailed, missing, createFailed, missing, createFailed, draft}, "", false, 4},
		{"never recreate published", "ensure-draft", []githubReply{published}, "", true, 0},
		{"create permission denied", "ensure-draft", []githubReply{missing, {"create", "HTTP 403: Forbidden", true}}, "", true, 0},
		{"upload", "upload", []githubReply{draft, {"upload", "", false}}, "", false, 0},
		{"upload partial failure", "upload", []githubReply{draft, {"upload", "HTTP 503: Service Unavailable", true}, draft, {"upload", "", false}}, "", false, 1},
		{"upload rate limited", "upload", []githubReply{draft, {"upload", "HTTP 429: Too Many Requests", true}, draft, {"upload", "", false}}, "", false, 1},
		{"never overwrite published", "upload", []githubReply{published}, "", true, 0},
		{"published during upload retry", "upload", []githubReply{draft, {"upload", "HTTP 502: Server Error", true}, published}, "", true, 1},
		{"missing upload target", "upload", []githubReply{missing}, "", true, 0},
		{"publish", "publish", []githubReply{draft, {"edit", "", false}}, "", false, 0},
		{"publish succeeded despite gateway error", "publish", []githubReply{draft, {"edit", "HTTP 502: Server Error", true}, published}, "", false, 1},
		{"publish failed before reaching server", "publish", []githubReply{draft, {"edit", "HTTP 503: Service Unavailable", true}, draft, {"edit", "", false}}, "", false, 1},
		{"already published", "publish", []githubReply{published}, "", false, 0},
		{"missing publish target", "publish", []githubReply{missing}, "", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var stub strings.Builder
			stub.WriteString("#!/usr/bin/env bash\nset -eu\nn=$(cat \"$RELEASE_TEST_DIR/count\")\nn=$((n+1))\necho \"$n\" > \"$RELEASE_TEST_DIR/count\"\ncase $n in\n")
			for i, reply := range tc.replies {
				args := "release " + reply.operation + " v0.4.2"
				switch reply.operation {
				case "view":
					args += " --json isDraft"
				case "create":
					args += " --verify-tag --draft --title Toolbox v0.4.2 --generate-notes"
				case "upload":
					args += " dist/binary dist/SHA256SUMS dist/SHA256SUMS.sigstore.json --clobber"
				case "edit":
					args += " --draft=false --latest"
				}
				fmt.Fprintf(&stub, "%d)\nif [[ \"$*\" != %s ]]; then echo unexpected > \"$RELEASE_TEST_DIR/unexpected\"; exit 90; fi\nprintf '%%s' %s", i+1, shellQuote(args), shellQuote(reply.output))
				if reply.failure {
					stub.WriteString(" >&2\nexit 1;;\n")
				} else {
					stub.WriteString("\nexit 0;;\n")
				}
			}
			stub.WriteString("*) echo unexpected > \"$RELEASE_TEST_DIR/unexpected\"; exit 91;;\nesac\n")
			writeTestFile(t, filepath.Join(dir, "gh"), stub.String(), 0755)
			writeTestFile(t, filepath.Join(dir, "count"), "0", 0600)
			writeTestFile(t, filepath.Join(dir, "sleep"), "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> \"$RELEASE_TEST_DIR/sleeps\"\n", 0755)
			args := []string{"github.sh", tc.command, "v0.4.2"}
			if tc.command == "upload" {
				args = append(args, "dist/binary", "dist/SHA256SUMS", "dist/SHA256SUMS.sigstore.json")
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "RELEASE_TEST_DIR="+dir)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if (err != nil) != tc.fail || string(out) != tc.want {
				t.Fatalf("output=%q error=%v stderr=%s", out, err, stderr.String())
			}
			count, err := os.ReadFile(filepath.Join(dir, "count"))
			if err != nil || strings.TrimSpace(string(count)) != strconv.Itoa(len(tc.replies)) {
				t.Fatalf("request count=%q error=%v stderr=%s", count, err, stderr.String())
			}
			if _, err := os.Stat(filepath.Join(dir, "unexpected")); !os.IsNotExist(err) {
				t.Fatal("unexpected GitHub command")
			}
			sleeps, _ := os.ReadFile(filepath.Join(dir, "sleeps"))
			var wantDelays []string
			for i := 1; i <= tc.sleeps; i++ {
				wantDelays = append(wantDelays, strconv.Itoa(1<<i))
			}
			if strings.Join(strings.Fields(string(sleeps)), ",") != strings.Join(wantDelays, ",") {
				t.Fatalf("retry delays=%q want=%v", sleeps, wantDelays)
			}
		})
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func writeTestFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}
