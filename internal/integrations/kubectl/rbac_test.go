package kubectl

import (
	"io"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestRBACMatchesReadCatalog(t *testing.T) {
	for _, tc := range []struct {
		file    string
		cluster bool
	}{{"reader.yaml", false}, {"cluster-reader.optional.yaml", true}} {
		f, err := os.Open("../../../deploy/kubernetes/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		d := yaml.NewDecoder(f)
		seen := map[string]bool{}
		for {
			var doc struct {
				Kind  string `yaml:"kind"`
				Rules []struct {
					Groups    []string `yaml:"apiGroups"`
					Resources []string `yaml:"resources"`
					Verbs     []string `yaml:"verbs"`
				} `yaml:"rules"`
			}
			err := d.Decode(&doc)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if !tc.cluster && doc.Kind == "ClusterRoleBinding" {
				t.Fatal("default role must be namespace bound")
			}
			for _, rule := range doc.Rules {
				for _, verb := range rule.Verbs {
					if verb != "get" && verb != "list" {
						t.Fatal("non-read RBAC verb", verb)
					}
				}
				for _, group := range rule.Groups {
					for _, resource := range rule.Resources {
						if strings.Contains(group, "*") || strings.Contains(resource, "*") {
							t.Fatal("wildcard authority")
						}
						if resource == "pods/log" && group == "" {
							continue
						}
						name := resource + "." + group
						found := false
						for _, r := range resources {
							if r.Name == name && r.ClusterScoped == tc.cluster {
								found = true
							}
						}
						if !found {
							t.Fatal("RBAC grants outside catalog", name)
						}
						seen[name] = true
					}
				}
			}
		}
		for _, r := range resources {
			if r.ClusterScoped == tc.cluster && !seen[r.Name] {
				t.Fatal("missing RBAC", r.Name)
			}
		}
	}
}

func TestOptionalForwardRBAC(t *testing.T) {
	b, err := os.ReadFile("../../../deploy/kubernetes/port-forward.optional.yaml")
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(b)))
	var role struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		Rules []struct {
			Groups    []string `yaml:"apiGroups"`
			Resources []string `yaml:"resources"`
			Verbs     []string `yaml:"verbs"`
		} `yaml:"rules"`
	}
	if decoder.Decode(&role) != nil || role.Kind != "Role" || role.Metadata.Namespace != "apps" || len(role.Rules) != 1 {
		t.Fatal(role)
	}
	rule := role.Rules[0]
	if len(rule.Groups) != 1 || rule.Groups[0] != "" || len(rule.Resources) != 1 || rule.Resources[0] != "pods/portforward" || strings.Join(rule.Verbs, ",") != "get,create" {
		t.Fatal(rule)
	}
	var binding struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		RoleRef struct{ Kind, Name string } `yaml:"roleRef"`
	}
	if decoder.Decode(&binding) != nil || binding.Kind != "RoleBinding" || binding.Metadata.Namespace != "apps" || binding.RoleRef.Kind != "Role" || binding.RoleRef.Name != "toolbox-port-forward" {
		t.Fatal(binding)
	}
}
