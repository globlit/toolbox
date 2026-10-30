package kubectl

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"

	"github.com/globlit/toolbox/internal/config"
	"go.yaml.in/yaml/v3"
)

// Strict decoding rejects exec, auth-provider, impersonation, proxy-url,
// insecure-skip-tls-verify, extensions and all unknown fields, even on unused entries.
type kubeConfig struct {
	APIVersion     string         `yaml:"apiVersion" json:"apiVersion"`
	Kind           string         `yaml:"kind" json:"kind"`
	CurrentContext string         `yaml:"current-context" json:"current-context"`
	Preferences    struct{}       `yaml:"preferences" json:"preferences"`
	Clusters       []namedCluster `yaml:"clusters" json:"clusters"`
	Users          []namedUser    `yaml:"users" json:"users"`
	Contexts       []namedContext `yaml:"contexts" json:"contexts"`
}
type cluster struct {
	Server string `yaml:"server" json:"server"`
	CAData string `yaml:"certificate-authority-data,omitempty" json:"certificate-authority-data,omitempty"`
	CAFile string `yaml:"certificate-authority,omitempty" json:"certificate-authority,omitempty"`
}
type user struct {
	Token    string `yaml:"token,omitempty" json:"token,omitempty"`
	CertData string `yaml:"client-certificate-data,omitempty" json:"client-certificate-data,omitempty"`
	KeyData  string `yaml:"client-key-data,omitempty" json:"client-key-data,omitempty"`
	CertFile string `yaml:"client-certificate,omitempty" json:"client-certificate,omitempty"`
	KeyFile  string `yaml:"client-key,omitempty" json:"client-key,omitempty"`
}
type kubeContext struct {
	Cluster   string `yaml:"cluster" json:"cluster"`
	User      string `yaml:"user" json:"user"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}
type namedCluster struct {
	Name    string  `yaml:"name" json:"name"`
	Cluster cluster `yaml:"cluster" json:"cluster"`
}
type namedUser struct {
	Name string `yaml:"name" json:"name"`
	User user   `yaml:"user" json:"user"`
}
type namedContext struct {
	Name    string      `yaml:"name" json:"name"`
	Context kubeContext `yaml:"context" json:"context"`
}

func credentialSnapshot(k config.Kubectl) ([]byte, []string, error) {
	b, err := config.ReadFile(k.Kubeconfig)
	if err != nil {
		return nil, nil, err
	}
	var source kubeConfig
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if d.Decode(&source) != nil || d.Decode(new(any)) != io.EOF {
		return nil, nil, errors.New("unsupported kubeconfig. Use one document with static credentials and verified TLS. Plugins, proxies, impersonation, and unknown fields are rejected")
	}
	if source.APIVersion != "v1" || source.Kind != "Config" {
		return nil, nil, errors.New("kubeconfig must be a v1 Config")
	}
	contexts := map[string]kubeContext{}
	clusters := map[string]cluster{}
	users := map[string]user{}
	for _, v := range source.Contexts {
		if _, ok := contexts[v.Name]; ok {
			return nil, nil, errors.New("duplicate kubeconfig context")
		}
		contexts[v.Name] = v.Context
	}
	for _, v := range source.Clusters {
		if _, ok := clusters[v.Name]; ok {
			return nil, nil, errors.New("duplicate kubeconfig cluster")
		}
		clusters[v.Name] = v.Cluster
	}
	for _, v := range source.Users {
		if _, ok := users[v.Name]; ok {
			return nil, nil, errors.New("duplicate kubeconfig user")
		}
		users[v.Name] = v.User
	}
	c, ok := contexts[k.Context]
	if !ok {
		return nil, nil, errors.New("configured context is missing")
	}
	cl, ok := clusters[c.Cluster]
	if !ok {
		return nil, nil, errors.New("configured cluster is missing")
	}
	u, ok := users[c.User]
	if !ok {
		return nil, nil, errors.New("configured user is missing")
	}
	endpoint, err := url.Parse(cl.Server)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return nil, nil, errors.New("cluster server must be a direct HTTPS origin")
	}
	for _, pair := range [][2]*string{{&cl.CAData, &cl.CAFile}, {&u.CertData, &u.CertFile}, {&u.KeyData, &u.KeyFile}} {
		if *pair[0] != "" && *pair[1] != "" {
			return nil, nil, errors.New("credential data uses both inline and file sources. Select one source")
		}
		if *pair[1] != "" {
			path := *pair[1]
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(k.Kubeconfig), path)
			}
			data, err := config.ReadFile(path)
			if err != nil {
				return nil, nil, err
			}
			*pair[0] = base64.StdEncoding.EncodeToString(data)
			*pair[1] = ""
		}
		if *pair[0] != "" {
			if _, err := base64.StdEncoding.DecodeString(*pair[0]); err != nil {
				return nil, nil, errors.New("invalid base64 credential data")
			}
		}
	}
	if (u.CertData == "") != (u.KeyData == "") || (u.Token == "" && u.CertData == "") || (u.Token != "" && u.CertData != "") {
		return nil, nil, errors.New("require either a static bearer token or a client certificate/key pair")
	}
	// Rebuild with fixed names. Never hand the original file to kubectl: it could
	// be changed after validation, and unused contexts are unnecessary authority.
	snapshot := kubeConfig{APIVersion: "v1", Kind: "Config", CurrentContext: "toolbox", Clusters: []namedCluster{{"toolbox", cl}}, Users: []namedUser{{"toolbox", u}}, Contexts: []namedContext{{"toolbox", kubeContext{Cluster: "toolbox", User: "toolbox", Namespace: k.DefaultNamespace}}}}
	result, err := json.Marshal(snapshot)
	literals := []string{}
	for _, s := range []string{u.Token, u.KeyData, u.CertData} {
		if s != "" {
			literals = append(literals, s)
		}
	}
	return result, literals, err
}
