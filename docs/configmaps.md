# ConfigMaps and namespaces

ConfigMap reads are enabled by default. Use `cm`, `configmap`, or `configmaps`. Ordinary `data` entries remain readable. The sanitizer replaces values under sensitive keys and masks recognized secrets, configured literals, patterns, and credentials. `binaryData` and annotations remain redacted.

```sh
toolbox --config /etc/toolbox/config.json kubectl get cm -n apps -o json
toolbox --config /etc/toolbox/config.json kubectl get configmaps -A
```

Configure namespace access:

```json
{
  "kubectl": {
    "binary": "/usr/local/bin/kubectl",
    "kubeconfig": "/etc/toolbox/reader.kubeconfig",
    "context": "toolbox-reader",
    "default_namespace": "apps",
    "namespaces": ["*"],
    "disable_configmaps": false,
    "configmap_namespaces": ["*"],
    "disabled_configmap_namespaces": ["private", "payments"]
  }
}
```

| Setting | Behavior |
| --- | --- |
| `namespaces` | Approved namespaces. `"*"` permits any valid namespace, subject to Kubernetes permissions. |
| `disable_configmaps` | `true` rejects every ConfigMap read. Default: `false`. |
| `configmap_namespaces` | Optional additional allowlist for ConfigMaps. Omitted, empty, or `["*"]` allows all approved namespaces. |
| `disabled_configmap_namespaces` | ConfigMap exclusions. Exclusions override the allowlist. `["*"]` excludes every namespace. |

Set `disable_configmaps: true` to disable ConfigMaps broadly. Namespace entries cannot override this setting. Other resources remain available.

For multiple contexts, put namespace selections and exclusions in each `contexts` entry. Set `kubectl.disable_configmaps: true` to disable ConfigMaps across every context. A context can also set its own `disable_configmaps: true`.

`"*"` works only as a complete configuration entry. Patterns such as `"prod-*"` are invalid. Requests still require a concrete namespace or `-A`. The default namespace must remain a concrete name. A ConfigMap allowlist cannot expand the main namespace selection.

## Data sanitization

JSON values and `.yaml` / `.yml` files receive structured sanitization. YAML output omits comments and masks anchors, aliases, unsupported tags, and malformed documents. Multiline plaintext and properties values receive text sanitization. Malformed entries do not discard other data entries. Matching multiline configured rules replace the affected entry.

This behavior applies to validated ConfigMap read endpoints, including named reads, lists, `-A`, and raw paths. Logs and other resource types do not gain this exception from a `kind: ConfigMap` field. Unknown or unlabeled secrets can still escape detection.

## All namespaces

With an explicit namespace list, `-A` queries only eligible namespaces. With `namespaces: ["*"]`, unrestricted reads use the Kubernetes cluster-wide list endpoint. Kubernetes must grant that list operation. Namespace RoleBindings alone do not grant cluster-wide list access. [Kubernetes permission scopes](https://kubernetes.io/docs/reference/access-authn-authz/rbac/).

For ConfigMaps, exclusions prevent cluster-wide fetching. Toolbox queries an explicit `configmap_namespaces` list when supplied. Otherwise, it lists namespaces first and queries each eligible namespace. This discovery requires Kubernetes permission to list namespaces. It does not enable user requests for cluster-scoped resources. Use explicit ConfigMap namespaces when the identity cannot list namespaces.

Any failed or incomplete read discards the combined result. Toolbox does not skip Kubernetes authorization failures. Raw cluster-wide ConfigMap paths are rejected when namespace restrictions require separate reads. Namespace-scoped raw paths enforce the same exclusions as normal reads.
