package kubectl

import (
	"net/url"
	"strings"
)

// configMapRead selects output handling from the validated request path.
func configMapRead(args []string) bool {
	if len(args) != 2 || args[0] != "get" || !strings.HasPrefix(args[1], "--raw=") {
		return false
	}
	u, err := url.ParseRequestURI(strings.TrimPrefix(args[1], "--raw="))
	if err != nil || u.Host != "" || u.Scheme != "" || u.RawPath != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) == 3 {
		return parts[0] == "api" && parts[1] == "v1" && parts[2] == "configmaps"
	}
	return (len(parts) == 5 || len(parts) == 6) && parts[0] == "api" && parts[1] == "v1" && parts[2] == "namespaces" && parts[4] == "configmaps"
}
