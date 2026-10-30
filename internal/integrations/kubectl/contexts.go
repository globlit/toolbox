package kubectl

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/globlit/toolbox/internal/config"
)

type ContextInput struct {
	Context string `json:"context,omitempty" jsonschema:"Approved context name. Required when multiple contexts are configured."`
}
type ContextCatalog struct {
	Contexts []config.Context `json:"contexts"`
}
type ResourceCatalog struct {
	Context    string     `json:"context"`
	Resources  []Resource `json:"resources"`
	Namespaces []string   `json:"namespaces"`
}

func (s *Service) Contexts() ContextCatalog {
	out := ContextCatalog{Contexts: []config.Context{}}
	for _, k := range s.settings.Scopes() {
		out.Contexts = append(out.Contexts, config.Context{Name: k.Context, DefaultNamespace: k.DefaultNamespace, Namespaces: append([]string{}, k.Namespaces...), AllowClusterScoped: k.AllowClusterScoped, AllowPortForward: k.AllowPortForward})
	}
	return out
}

func (s *Service) selectContext(name string) (*Service, error) {
	if s.contexts == nil {
		if name == "" || name == s.settings.Context {
			return s, nil
		}
	} else {
		if name == "" {
			if len(s.contexts) != 1 {
				return nil, errors.New("context is required. List approved contexts with toolbox kubectl contexts or kubectl_contexts")
			}
			for _, scoped := range s.contexts {
				return scoped, nil
			}
		}
		if scoped, ok := s.contexts[name]; ok {
			return scoped, nil
		}
	}
	return nil, errors.New("context is not permitted")
}

func (s *Service) ResourceCatalog(in ContextInput) (ResourceCatalog, error) {
	scoped, err := s.selectContext(in.Context)
	if err != nil {
		return ResourceCatalog{}, err
	}
	return ResourceCatalog{Context: scoped.settings.Context, Resources: Catalog(scoped.settings), Namespaces: append([]string{}, scoped.settings.Namespaces...)}, nil
}

func (s *Service) getAll(ctx context.Context, in GetInput) (Result, error) {
	if in.Namespace != "" || in.Name != "" {
		return Result{}, errors.New("all_namespaces cannot be combined with namespace or name")
	}
	in.AllNamespaces = false
	plans := make([][]string, 0, len(s.settings.Namespaces))
	for _, ns := range s.settings.Namespaces {
		in.Namespace = ns
		args, err := getArgs(s.settings, in)
		if err != nil {
			return Result{}, err
		}
		plans = append(plans, args)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.settings.TimeoutSeconds)*time.Second)
	defer cancel()
	result := Result{Context: s.settings.Context, Namespaces: append([]string{}, s.settings.Namespaces...), Untrusted: true}
	items := []json.RawMessage{}
	used := 0
	for _, args := range plans {
		if ctx.Err() != nil {
			return Result{}, errors.New("request cancelled or timed out. Combined response discarded")
		}
		out, err := s.execute(ctx, args, true)
		if err != nil {
			return Result{}, err
		}
		if out.Error != "" {
			out.Namespaces = result.Namespaces
			out.Data = nil
			return s.checkResult(out)
		}
		encoded, _ := json.Marshal(out)
		used += len(encoded)
		if used > s.settings.MaxOutputBytes {
			return Result{}, errors.New("combined output exceeded the limit. Response discarded. Narrow the query")
		}
		var list struct {
			Items    []json.RawMessage `json:"items"`
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
		}
		if json.Unmarshal(out.Data, &list) != nil || list.Items == nil || list.Metadata.Continue != "" {
			return Result{}, errors.New("expected a complete JSON resource list. Combined response discarded")
		}
		items = append(items, list.Items...)
		result.Redactions += out.Redactions
		result.WithheldLines += out.WithheldLines
		if out.Diagnostics != "" {
			if result.Diagnostics != "" {
				result.Diagnostics += "\n"
			}
			result.Diagnostics += out.Diagnostics
		}
	}
	if ctx.Err() != nil {
		return Result{}, errors.New("request cancelled or timed out. Combined response discarded")
	}
	result.Data, _ = json.Marshal(struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Items      []json.RawMessage `json:"items"`
	}{"v1", "List", items})
	return s.checkResult(result)
}
