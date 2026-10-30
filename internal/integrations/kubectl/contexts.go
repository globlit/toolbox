package kubectl

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
		out.Contexts = append(out.Contexts, config.Context{Name: k.Context, DefaultNamespace: k.DefaultNamespace, Namespaces: append([]string{}, k.Namespaces...), AllowClusterScoped: k.AllowClusterScoped, AllowPortForward: k.AllowPortForward, DisableConfigMaps: k.DisableConfigMaps, ConfigMapNamespaces: append([]string{}, k.ConfigMapNamespaces...), DisabledConfigMapNamespaces: append([]string{}, k.DisabledConfigMapNamespaces...)})
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
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.settings.TimeoutSeconds)*time.Second)
	defer cancel()
	namespaces := s.settings.Namespaces
	used := 0
	cm := in.Resource == "configmaps." || aliases[in.Resource] == "configmaps."
	if cm && (s.settings.DisableConfigMaps || slices.Contains(s.settings.DisabledConfigMapNamespaces, "*")) {
		return Result{}, errors.New("ConfigMaps are disabled")
	}
	if slices.Contains(namespaces, "*") {
		if !cm || s.settings.ConfigMapsEverywhere() {
			namespaces = []string{"*"}
		} else if len(s.settings.ConfigMapNamespaces) > 0 && !slices.Contains(s.settings.ConfigMapNamespaces, "*") {
			namespaces = s.settings.ConfigMapNamespaces
		} else {
			var err error
			namespaces, used, err = s.discoverNamespaces(ctx)
			if err != nil {
				return Result{}, err
			}
		}
	}
	selected := []string{}
	plans := make([][]string, 0, len(namespaces))
	for _, ns := range namespaces {
		if cm && !s.settings.ConfigMapsAllowed(ns) {
			continue
		}
		in.Namespace = ns
		in.AllNamespaces = ns == "*"
		if in.AllNamespaces {
			in.Namespace = ""
		}
		args, err := getArgs(s.settings, in)
		if err != nil {
			return Result{}, err
		}
		plans = append(plans, args)
		selected = append(selected, ns)
	}
	if len(plans) == 0 {
		return Result{}, errors.New("no permitted namespaces for this resource")
	}
	result := Result{Context: s.settings.Context, Namespaces: selected, Untrusted: true}
	items := []json.RawMessage{}
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

// discoverNamespaces supports wildcard ConfigMap reads with namespace exclusions.
func (s *Service) discoverNamespaces(ctx context.Context) ([]string, int, error) {
	out, err := s.execute(ctx, []string{"get", "--raw=/api/v1/namespaces"}, true)
	if err != nil {
		return nil, 0, err
	}
	if out.Error != "" {
		return nil, 0, errors.New("cannot list namespaces for ConfigMap exclusions. Use explicit configmap_namespaces")
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
		Metadata struct {
			Continue string `json:"continue"`
		} `json:"metadata"`
	}
	if json.Unmarshal(out.Data, &list) != nil || list.Items == nil || list.Metadata.Continue != "" {
		return nil, 0, errors.New("expected a complete namespace list")
	}
	namespaces := make([]string, 0, len(list.Items))
	seen := map[string]bool{}
	for _, item := range list.Items {
		ns := item.Metadata.Name
		if !config.Namespace(ns) || seen[ns] {
			return nil, 0, errors.New("invalid namespace list")
		}
		seen[ns] = true
		namespaces = append(namespaces, ns)
	}
	encoded, _ := json.Marshal(out)
	return namespaces, len(encoded), nil
}
