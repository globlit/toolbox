package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"toolbox/internal/config"
	"toolbox/internal/integrations/kubectl"
	transport "toolbox/internal/transport/mcp"
)

const Help = `toolbox 0.2.0 - read-only agent gateway

  toolbox mcp
  toolbox kubectl resources
  toolbox kubectl get RESOURCE [NAME] [-n NAMESPACE] [-l SELECTOR]
  toolbox kubectl logs POD [-n NAMESPACE] [-c CONTAINER] [--previous] [--tail N] [--since 1h]
  toolbox version

Both transports share a closed read policy and mandatory sanitizer. Output is JSON.
This is not a kubectl passthrough. Unknown commands/flags are rejected.
Configuration is pinned by an operator build; --config and environment overrides are rejected.
Operator-owned static credentials and least-privilege RBAC are required.
`

type request struct {
	mode string
	get  kubectl.GetInput
	logs kubectl.LogsInput
}

func parse(args []string) (request, error) {
	var r request
	if len(args) == 1 && args[0] == "mcp" {
		r.mode = "mcp"
		return r, nil
	}
	if len(args) < 2 || args[0] != "kubectl" {
		return r, errors.New("expected kubectl resources, get, or logs")
	}
	r.mode = args[1]
	args = args[2:]
	if r.mode == "resources" && len(args) == 0 {
		return r, nil
	}
	if r.mode != "get" && r.mode != "logs" {
		return r, errors.New("command is not permitted")
	}
	var positional []string
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		key, value, hasValue := strings.Cut(a, "=")
		switch key {
		case "-n":
			key = "--namespace"
		case "-l":
			key = "--selector"
		case "-c":
			key = "--container"
		}
		if seen[key] {
			return r, errors.New("duplicate option is not permitted")
		}
		seen[key] = true
		if key == "--previous" && r.mode == "logs" {
			if hasValue {
				return r, errors.New("--previous takes no value")
			}
			r.logs.Previous = true
			continue
		}
		allowed := key == "--namespace" || (r.mode == "get" && key == "--selector") || (r.mode == "logs" && (key == "--container" || key == "--tail" || key == "--since"))
		if !allowed {
			return r, errors.New("option is not permitted")
		}
		if !hasValue {
			i++
			if i >= len(args) {
				return r, errors.New("option requires a value")
			}
			value = args[i]
		}
		if value == "" || strings.HasPrefix(value, "-") {
			return r, errors.New("invalid option value")
		}
		switch key {
		case "--namespace":
			r.get.Namespace = value
			r.logs.Namespace = value
		case "--selector":
			r.get.Selector = value
		case "--container":
			r.logs.Container = value
		case "--since":
			r.logs.Since = value
		case "--tail":
			n, err := strconv.Atoi(value)
			if err != nil {
				return r, errors.New("tail must be an integer")
			}
			r.logs.Tail = n
		}
	}
	if r.mode == "get" && len(positional) >= 1 && len(positional) <= 2 {
		r.get.Resource = positional[0]
		if len(positional) == 2 {
			r.get.Name = positional[1]
		}
		return r, nil
	}
	if r.mode == "logs" && len(positional) == 1 {
		r.logs.Pod = positional[0]
		return r, nil
	}
	return r, errors.New("invalid number of arguments")
}

// Run never prints parser errors containing agent-supplied values or file contents.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return run(ctx, args, stdout, stderr, config.LoadOperator)
}

// Private loader injection is used only by unit tests, not by the executable.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, load func() (config.Config, error)) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "help" || args[0] == "-h")) {
		fmt.Fprint(stdout, Help)
		return 0
	}
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, "toolbox 0.2.0")
		return 0
	}
	r, err := parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	c, err := load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	service, err := kubectl.New(c)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if r.mode == "mcp" {
		if err := transport.Serve(ctx, service); err != nil {
			fmt.Fprintln(stderr, "MCP server stopped")
			return 1
		}
		return 0
	}
	var out any
	exit := 0
	switch r.mode {
	case "resources":
		out = transport.Catalog{Resources: service.Resources(), Namespaces: service.Namespaces()}
	case "get":
		var result kubectl.Result
		result, err = service.Get(ctx, r.get)
		out = result
		if result.Error != "" {
			exit = 1
		}
	case "logs":
		var result kubectl.Result
		result, err = service.Logs(ctx, r.logs)
		out = result
		if result.Error != "" {
			exit = 1
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return 1
	}
	return exit
}
