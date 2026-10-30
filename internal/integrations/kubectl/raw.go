package kubectl

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"toolbox/internal/config"
)

type RawInput struct {
	Context string `json:"context,omitempty" jsonschema:"Approved context name. Required when multiple contexts are configured."`
	Path    string `json:"path" jsonschema:"Absolute Kubernetes API path. Only approved resource and discovery GET endpoints are permitted."`
}

// rawArgs accepts canonical read paths and rebuilds the query before execution.
func rawArgs(k config.Kubectl, in RawInput) ([]string, error) {
	denied := errors.New("raw path is not permitted. Use an approved resource or discovery GET endpoint")
	if len(in.Path) > 4096 || !strings.HasPrefix(in.Path, "/") || strings.ContainsAny(in.Path, "\\#\r\n\t\x00") {
		return nil, denied
	}
	u, err := url.ParseRequestURI(in.Path)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" || u.RawPath != "" || strings.Contains(u.Path, "%") || u.EscapedPath() != u.Path {
		return nil, denied
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, denied
		}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, denied
	}
	for _, values := range q {
		if len(values) != 1 {
			return nil, denied
		}
	}
	if u.Path == "/version" || u.Path == "/api" || u.Path == "/apis" {
		if len(q) != 0 {
			return nil, denied
		}
		return []string{"get", "--raw=" + u.Path}, nil
	}
	for _, resource := range Catalog(k) {
		base := "/apis/" + resource.APIVersion
		if resource.APIVersion == "v1" {
			base = "/api/v1"
		}
		if u.Path == base || (strings.HasPrefix(base, "/apis/") && u.Path == strings.TrimSuffix(base, "/"+strings.SplitN(resource.APIVersion, "/", 2)[1])) {
			if len(q) != 0 {
				return nil, denied
			}
			return []string{"get", "--raw=" + u.Path}, nil
		}
		prefix := base + "/"
		if !strings.HasPrefix(u.Path, prefix) {
			continue
		}
		rest := strings.Split(strings.TrimPrefix(u.Path, prefix), "/")
		input := GetInput{Resource: resource.Name}
		if !resource.ClusterScoped {
			if len(rest) < 3 || rest[0] != "namespaces" {
				continue
			}
			input.Namespace = rest[1]
			rest = rest[2:]
		}
		if len(rest) < 1 || rest[0] != strings.SplitN(resource.Name, ".", 2)[0] || len(rest) > 2 {
			continue
		}
		if len(rest) == 2 {
			input.Name = rest[1]
		}
		input.Selector = q.Get("labelSelector")
		args, err := getArgs(k, input)
		if err != nil {
			return nil, err
		}
		for key, values := range q {
			switch key {
			case "labelSelector":
			case "fieldSelector":
				if len(values[0]) > 512 || !selectorRE.MatchString(values[0]) || strings.HasPrefix(values[0], "-") {
					return nil, denied
				}
			case "limit":
				n, err := strconv.Atoi(values[0])
				if err != nil || n < 1 || n > 2000 || input.Name != "" {
					return nil, denied
				}
			default:
				return nil, denied
			}
		}
		// getArgs validates the scope and resource. Only validated query keys survive.
		args[1] = "--raw=" + u.Path
		if len(q) != 0 {
			args[1] += "?" + q.Encode()
		}
		return args, nil
	}
	return nil, denied
}
