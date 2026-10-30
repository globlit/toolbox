package kubectl

import (
	"net/url"
	"strings"
	"testing"
)

func TestAuditPolicyInvariants(t *testing.T) {
	k := settings()
	k.AllowClusterScoped = true
	for _, r := range Catalog(k) {
		input := GetInput{Resource: r.Name, Name: "safe-name", Selector: "app in (api,worker),example.org/name!=foo"}
		args, err := getArgs(k, input)
		if err != nil {
			t.Fatal(r, err)
		}
		if len(args) != 2 || args[0] != "get" || !strings.HasPrefix(args[1], "--raw=/") {
			t.Fatal(args)
		}
		u, err := url.Parse(strings.TrimPrefix(args[1], "--raw="))
		if err != nil || u.Host != "" || u.Scheme != "" || u.Fragment != "" {
			t.Fatal(args)
		}
		base := "/apis/" + r.APIVersion
		if r.APIVersion == "v1" {
			base = "/api/v1"
		}
		if !r.ClusterScoped {
			base += "/namespaces/apps"
		}
		want := base + "/" + strings.SplitN(r.Name, ".", 2)[0] + "/safe-name"
		if u.Path != want || u.Query().Get("labelSelector") != input.Selector || len(u.Query()) != 1 {
			t.Fatal(args)
		}
	}
	payloads := []string{"../secrets", "..", "%2e%2e%2fsecrets", "x%252f..", "x/exec", "x?watch=true", "x#frag", "x\x00", "x\n--raw=/api/v1/secrets", "--as=admin", "-A", "https://evil", "x;delete", "x\\..\\secrets"}
	for _, p := range payloads {
		if _, e := getArgs(k, GetInput{Resource: "pods", Name: p}); e == nil {
			t.Fatal("name accepted", p)
		}
		if _, e := getArgs(k, GetInput{Resource: p}); e == nil {
			t.Fatal("resource accepted", p)
		}
		if _, e := logsArgs(k, LogsInput{Pod: p}); e == nil {
			t.Fatal("pod accepted", p)
		}
		if _, e := logsArgs(k, LogsInput{Pod: "safe", Container: p}); e == nil {
			t.Fatal("container accepted", p)
		}
	}
	t.Logf("verified %d fixed catalog routes and %d adversarial fields", len(Catalog(k)), len(payloads)*4)
}
