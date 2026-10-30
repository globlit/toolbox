// Package kubectl exposes a finite read API, never a kubectl command passthrough.
package kubectl

import (
	"errors"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/globlit/toolbox/internal/config"
)

type Resource struct {
	Name          string `json:"name"`
	APIVersion    string `json:"api_version"`
	ClusterScoped bool   `json:"cluster_scoped"`
}

// Names and versions are fixed. Reads use exact API paths; kubectl discovery
// resolution can otherwise fall back to a different API group.
var resources = []Resource{
	{"pods.", "v1", false}, {"services.", "v1", false}, {"endpoints.", "v1", false}, {"events.", "v1", false}, {"persistentvolumeclaims.", "v1", false},
	{"deployments.apps", "apps/v1", false}, {"statefulsets.apps", "apps/v1", false}, {"daemonsets.apps", "apps/v1", false}, {"replicasets.apps", "apps/v1", false},
	{"jobs.batch", "batch/v1", false}, {"cronjobs.batch", "batch/v1", false}, {"endpointslices.discovery.k8s.io", "discovery.k8s.io/v1", false},
	{"ingresses.networking.k8s.io", "networking.k8s.io/v1", false}, {"networkpolicies.networking.k8s.io", "networking.k8s.io/v1", false},
	{"horizontalpodautoscalers.autoscaling", "autoscaling/v2", false}, {"poddisruptionbudgets.policy", "policy/v1", false},
	{"certificates.cert-manager.io", "cert-manager.io/v1", false}, {"certificaterequests.cert-manager.io", "cert-manager.io/v1", false}, {"issuers.cert-manager.io", "cert-manager.io/v1", false},
	{"clusters.postgresql.cnpg.io", "postgresql.cnpg.io/v1", false}, {"backups.postgresql.cnpg.io", "postgresql.cnpg.io/v1", false}, {"scheduledbackups.postgresql.cnpg.io", "postgresql.cnpg.io/v1", false}, {"poolers.postgresql.cnpg.io", "postgresql.cnpg.io/v1", false},
	{"rabbitmqclusters.rabbitmq.com", "rabbitmq.com/v1beta1", false}, {"opensearchclusters.opensearch.opster.io", "opensearch.opster.io/v1", false},
	{"prometheuses.monitoring.coreos.com", "monitoring.coreos.com/v1", false}, {"alertmanagers.monitoring.coreos.com", "monitoring.coreos.com/v1", false}, {"servicemonitors.monitoring.coreos.com", "monitoring.coreos.com/v1", false}, {"podmonitors.monitoring.coreos.com", "monitoring.coreos.com/v1", false}, {"prometheusrules.monitoring.coreos.com", "monitoring.coreos.com/v1", false},
	{"ciliumendpoints.cilium.io", "cilium.io/v2", false}, {"ciliumnetworkpolicies.cilium.io", "cilium.io/v2", false},
	{"nodes.", "v1", true}, {"namespaces.", "v1", true}, {"persistentvolumes.", "v1", true}, {"storageclasses.storage.k8s.io", "storage.k8s.io/v1", true},
	{"clusterissuers.cert-manager.io", "cert-manager.io/v1", true}, {"ciliumnodes.cilium.io", "cilium.io/v2", true}, {"ciliumclusterwidenetworkpolicies.cilium.io", "cilium.io/v2", true},
}

var aliases = map[string]string{
	"po": "pods.", "pod": "pods.", "pods": "pods.", "svc": "services.", "service": "services.", "services": "services.",
	"events": "events.", "event": "events.", "ev": "events.", "endpoints": "endpoints.", "ep": "endpoints.", "pvc": "persistentvolumeclaims.", "persistentvolumeclaims": "persistentvolumeclaims.",
	"deploy": "deployments.apps", "deployment": "deployments.apps", "deployments": "deployments.apps", "sts": "statefulsets.apps", "statefulsets": "statefulsets.apps", "ds": "daemonsets.apps", "daemonsets": "daemonsets.apps", "rs": "replicasets.apps", "replicasets": "replicasets.apps",
	"jobs": "jobs.batch", "job": "jobs.batch", "cronjobs": "cronjobs.batch", "cj": "cronjobs.batch", "ingresses": "ingresses.networking.k8s.io", "ing": "ingresses.networking.k8s.io", "hpa": "horizontalpodautoscalers.autoscaling", "pdb": "poddisruptionbudgets.policy",
	"nodes": "nodes.", "node": "nodes.", "no": "nodes.", "namespaces": "namespaces.", "ns": "namespaces.", "pv": "persistentvolumes.", "persistentvolumes": "persistentvolumes.", "sc": "storageclasses.storage.k8s.io", "storageclasses": "storageclasses.storage.k8s.io",
}

type GetInput struct {
	AllNamespaces bool   `json:"all_namespaces,omitempty" jsonschema:"List across approved namespaces in the selected context. Incompatible with namespace, name, and cluster-scoped resources."`
	Context       string `json:"context,omitempty" jsonschema:"Approved context name. Required when multiple contexts are configured."`
	Resource      string `json:"resource" jsonschema:"Resource name from kubectl_resources or a supported alias."`
	Name          string `json:"name,omitempty" jsonschema:"Single object name. Omit to list objects."`
	Namespace     string `json:"namespace,omitempty" jsonschema:"Approved namespace. Defaults to the configured namespace. Omit for cluster-scoped resources."`
	Selector      string `json:"selector,omitempty" jsonschema:"Label selector. Maximum 512 bytes."`
}

type LogsInput struct {
	Context   string `json:"context,omitempty" jsonschema:"Approved context name. Required when multiple contexts are configured."`
	Pod       string `json:"pod" jsonschema:"Single pod name. Workload targets and URLs are unsupported."`
	Namespace string `json:"namespace,omitempty" jsonschema:"Approved namespace. Defaults to the configured namespace."`
	Container string `json:"container,omitempty" jsonschema:"Container name. Required when Kubernetes cannot select a default."`
	Previous  bool   `json:"previous,omitempty" jsonschema:"Logs from the previous container instance. Defaults to false."`
	Tail      int    `json:"tail,omitempty" jsonschema:"Log lines from 1 to 2000. Omit or use 0 for the default of 200."`
	Since     string `json:"since,omitempty" jsonschema:"Lookback duration from 1s to 24h. Defaults to 1h. Fractional seconds round up."`
}

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var selectorRE = regexp.MustCompile(`^[A-Za-z0-9_./=!,() -]+$`)

func validName(s string) bool {
	return len(s) > 0 && len(s) <= 253 && nameRE.MatchString(s) && !strings.Contains(s, "..")
}

func namespace(k config.Kubectl, ns string) (string, error) {
	if ns == "" {
		ns = k.DefaultNamespace
	}
	if !config.Namespace(ns) || !slices.Contains(k.Namespaces, ns) {
		return "", errors.New("namespace is not permitted")
	}
	return ns, nil
}

func Catalog(k config.Kubectl) []Resource {
	out := []Resource{}
	for _, r := range resources {
		if !r.ClusterScoped || k.AllowClusterScoped {
			out = append(out, r)
		}
	}
	return out
}

func getArgs(k config.Kubectl, in GetInput) ([]string, error) {
	if in.AllNamespaces {
		return nil, errors.New("all_namespaces requires namespace fan-out")
	}
	r := in.Resource
	if a, ok := aliases[r]; ok {
		r = a
	}
	idx := slices.IndexFunc(Catalog(k), func(x Resource) bool { return x.Name == r })
	if idx < 0 {
		return nil, errors.New("resource is not permitted. Use toolbox kubectl resources or kubectl_resources for the catalog")
	}
	if in.Name != "" && !validName(in.Name) {
		return nil, errors.New("invalid object name")
	}
	if len(in.Selector) > 512 || (in.Selector != "" && (!selectorRE.MatchString(in.Selector) || strings.HasPrefix(in.Selector, "-"))) {
		return nil, errors.New("invalid label selector")
	}
	resource := Catalog(k)[idx]
	base := "/apis/" + resource.APIVersion
	if resource.APIVersion == "v1" {
		base = "/api/v1"
	}
	if resource.ClusterScoped {
		if in.Namespace != "" {
			return nil, errors.New("namespace cannot be supplied for a cluster-scoped resource")
		}
	} else {
		ns, err := namespace(k, in.Namespace)
		if err != nil {
			return nil, err
		}
		base += "/namespaces/" + ns
	}
	base += "/" + strings.SplitN(resource.Name, ".", 2)[0]
	if in.Name != "" {
		base += "/" + in.Name
	}
	query := url.Values{}
	if in.Selector != "" {
		query.Set("labelSelector", in.Selector)
	}
	if len(query) > 0 {
		base += "?" + query.Encode()
	}
	args := []string{"get", "--raw=" + base}
	return args, nil
}

func logsArgs(k config.Kubectl, in LogsInput) ([]string, error) {
	if !validName(in.Pod) || (in.Container != "" && (!config.Namespace(in.Container))) {
		return nil, errors.New("invalid pod or container name")
	}
	ns, err := namespace(k, in.Namespace)
	if err != nil {
		return nil, err
	}
	if in.Tail == 0 {
		in.Tail = 200
	}
	if in.Tail < 1 || in.Tail > 2000 {
		return nil, errors.New("tail must be between 1 and 2000 lines. Use 0 for the default of 200")
	}
	if in.Since == "" {
		in.Since = "1h"
	}
	d, err := time.ParseDuration(in.Since)
	if err != nil || d < time.Second || d > 24*time.Hour {
		return nil, errors.New("since must be a duration from 1s to 24h")
	}
	// Use a fixed GET endpoint, bypassing discovery and workload resolution.
	// No limitBytes: partial records could hide the identifying part of a secret.
	query := url.Values{
		"tailLines":    {strconv.Itoa(in.Tail)},
		"sinceSeconds": {strconv.FormatInt(int64(math.Ceil(d.Seconds())), 10)},
		"follow":       {"false"}, "timestamps": {"false"},
		"previous": {strconv.FormatBool(in.Previous)},
	}
	if in.Container != "" {
		query.Set("container", in.Container)
	}
	args := []string{"get", "--raw=/api/v1/namespaces/" + ns + "/pods/" + in.Pod + "/log?" + query.Encode()}
	return args, nil
}
