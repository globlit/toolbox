# Setup

Use an existing read-only kubeconfig with Toolbox. Toolbox does not create Kubernetes identities or apply permission manifests.

## Prerequisites

- Installed Toolbox executable, or Go 1.26.10 or newer to build from source.
- Native kubectl executable on the runtime host.
- Existing kubeconfig with a static bearer token or client certificate/key pair.
- Read access to the target resources through Kubernetes role-based access control (RBAC).
- Operator-controlled paths for the executable, configuration, and credentials.

The examples use these locations and names. Replace them before running the commands.

| Example | Meaning |
| --- | --- |
| `/usr/local/bin/kubectl` | Approved native kubectl executable |
| `/etc/toolbox/reader.kubeconfig` | Existing service-account kubeconfig |
| `apps-reader` | Context name in that kubeconfig |
| `apps` | Namespace to expose |
| `/etc/toolbox/toolbox.json` | Toolbox configuration file |
| `/opt/toolbox/bin/toolbox` | Output executable |

## 1. Select the existing identity

Reuse the existing ServiceAccount, Role, and RoleBinding. Do not apply the example manifests if those resources already provide the required access.

The kubeconfig credential determines the Kubernetes identity. Its `users[].name` is a local reference, not a service-account assignment. The selected context links that reference to a cluster. Toolbox uses approved contexts and namespace lists. It ignores the kubeconfig's current context and default namespace.

List context names without printing credentials:

```sh
kubectl --kubeconfig /etc/toolbox/reader.kubeconfig config get-contexts -o name
```

Select the intended context from the output. Do not paste token or certificate contents into an agent conversation.

Check the credential format before continuing:

| Accepted | Rejected |
| --- | --- |
| Inline bearer token | `exec`, `auth-provider`, `tokenFile` |
| Client certificate/key pair, inline or file references | Impersonation fields |
| HTTPS server with verified certificates | `insecure-skip-tls-verify`, `proxy-url` |
| One YAML or JSON `v1 Config` document | Unknown fields, extensions, duplicate names |

The loader checks unknown fields across the entire file, including unused entries. If another context uses unsupported authentication, prepare a separate minimal kubeconfig for the approved identity. Preserve its cluster certificate authority and credential references. Toolbox does not merge files from `KUBECONFIG` or refresh credentials through cloud login helpers.

## 2. Check permissions

Run these operator checks with the existing kubeconfig. They query authorization and do not perform the requested resource actions. Toolbox itself does not expose `auth can-i`. [Command reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_auth/kubectl_auth_can-i/).

```sh
kubectl --kubeconfig /etc/toolbox/reader.kubeconfig --context apps-reader auth can-i list pods -n apps
kubectl --kubeconfig /etc/toolbox/reader.kubeconfig --context apps-reader auth can-i get pods -n apps
kubectl --kubeconfig /etc/toolbox/reader.kubeconfig --context apps-reader auth can-i get pods --subresource=log -n apps
kubectl --kubeconfig /etc/toolbox/reader.kubeconfig --context apps-reader auth can-i delete pods -n apps
kubectl --kubeconfig /etc/toolbox/reader.kubeconfig --context apps-reader auth can-i get secrets -n apps
```

Expected results for this example:

```text
yes
yes
yes
no
no
```

These results are requirements, not observations from your cluster. If an intended read returns `no`, ask the cluster operator to review that permission. If a prohibited action returns `yes`, remove the excessive grant before deployment. A denied check exits with a nonzero status.

Review all RoleBindings and ClusterRoleBindings affecting the identity. Repeat checks for each permitted namespace and resource. These five checks do not prove that every write is denied. Kubernetes RBAC permissions are additive. [RBAC reference](https://kubernetes.io/docs/reference/access-authn-authz/rbac/).

## 3. Configure Toolbox

Save this JSON at the approved Toolbox configuration path:

```json
{
  "kubectl": {
    "binary": "/usr/local/bin/kubectl",
    "kubeconfig": "/etc/toolbox/reader.kubeconfig",
    "context": "apps-reader",
    "default_namespace": "apps",
    "namespaces": ["apps"],
    "allow_cluster_scoped": false,
    "timeout_seconds": 30,
    "max_output_bytes": 1048576
  },
  "redaction": {
    "literals": [],
    "patterns": []
  }
}
```

Set absolute file paths. Include `default_namespace` in `namespaces`, or use `["*"]` for all namespaces. Namespace entries restrict Toolbox requests but grant no Kubernetes permissions. Read [ConfigMap exclusions and wildcard namespaces](configmaps.md). Leave cluster-scoped access disabled unless the operator approves it and provides the corresponding permissions.

### Multiple contexts

Use the [multiple-context configuration](../config/toolbox.multicontext.example.json) for one kubeconfig with several approved contexts. Replace the single-context fields (`context`, `default_namespace`, `namespaces`, `allow_cluster_scoped`, `allow_port_forward`) with `contexts`:

```json
{
  "contexts": [
    {
      "name": "prod-a",
      "default_namespace": "apps",
      "namespaces": ["apps", "observability"],
      "allow_cluster_scoped": false,
      "allow_port_forward": false
    },
    {
      "name": "prod-b",
      "default_namespace": "billing",
      "namespaces": ["billing"],
      "allow_cluster_scoped": false,
      "allow_port_forward": false
    }
  ]
}
```

Match each `name` to an existing kubeconfig context. Configure 1–100 contexts with 1–100 unique namespace entries each. Use `["*"]` for all namespaces. With `--config`, new CLI invocations read configuration edits. Restart MCP to load edits.

```sh
toolbox kubectl contexts
toolbox kubectl get pods --context prod-a -A -o json
toolbox kubectl --context prod-b get pods -n billing
toolbox kubectl logs api-0 --context prod-a -n apps
```

For MCP, call `kubectl_contexts` to list approved contexts. Select a context in each read, resource-catalog, or tunnel-start request:

```json
{"name":"kubectl_get","arguments":{"context":"prod-a","resource":"pods","all_namespaces":true}}
```

Read [context selection and query examples](contexts.md) for catalog output, per-request selection, and cross-context queries.

Multiple configured contexts require explicit selection. One configured context remains the default. Requests do not change kubeconfig or a shared active context. Results identify the selected context. Tunnel status remains tied to its creation context.

`-A` / `--all-namespaces` queries approved namespaces in the selected context. With `["*"]`, unrestricted reads use the cluster-wide list endpoint. ConfigMap exclusions require separate namespace reads. Results report the queried namespaces or `["*"]`. The option rejects `-n`, object names, and cluster-scoped resources. A failed or incomplete read discards the combined data.

`-o json` / `--output=json` accepts the existing sanitized result envelope, including `data`, `context`, and redaction counts. Other output formats remain unsupported. MCP already returns JSON.

## 4. Install and verify

Use [Go installation or a verified release binary](install.md). To build from source instead, create the output directory and run:

```sh
go build -o /opt/toolbox/bin/toolbox ./cmd/toolbox
/opt/toolbox/bin/toolbox --config /etc/toolbox/toolbox.json kubectl get pods -n apps
```

A successful read returns JSON with `context`, `data`, `redactions`, and `untrusted`. An empty namespace returns an empty `items` array. For multiple contexts, add `--context NAME`. If the command fails, check file access, credentials, certificate trust, and resource permissions.

Register the executable using [client configuration](clients.md).

### Runtime configuration

Supply `--config FILE` or `--config=FILE` before or after the command:

```sh
toolbox --config /etc/toolbox/toolbox.json kubectl contexts
toolbox kubectl get pods --context prod-a -A -o json --config=./toolbox.json
toolbox mcp --config /etc/toolbox/toolbox.json
```

Absolute and relative configuration paths are accepted. Relative paths resolve from the process's working directory. Paths inside the JSON for `binary` and `kubeconfig` must remain absolute. Use an absolute `--config` path in MCP client registration.

Toolbox accepts any valid local configuration supplied by the caller. No operator build or configuration digest is required for `--config`. The file selects the executable, credentials, approved contexts, namespace lists, and optional tunnel permissions. Trust the caller and configuration author. Built-in command restrictions and mandatory sanitization still apply.

The MCP server loads configuration at startup. Individual MCP calls cannot supply `config`, a kubeconfig path, or executable overrides. Restart the server after editing its configuration. `KUBECONFIG`, `TOOLBOX_CONFIG`, and `TOOLBOX_CONFIG_PATH` do not select configuration.

### Optional built-in default

Build a default configuration into the executable to omit `--config`:

```sh
go run ./scripts/provision -config /etc/toolbox/toolbox.json -out /opt/toolbox/bin/toolbox
/opt/toolbox/bin/toolbox kubectl get pods -n apps
```

Successful provisioning prints `Gateway build succeeded.` The default loader verifies the configuration and kubectl digests recorded during provisioning. Rebuild when those files change. Explicit `--config` takes precedence over this default and bypasses its recorded digests. It hashes the selected executable at startup and checks that digest before each subprocess launch.

An ordinary build without `--config` fails with `configuration required. Use --config FILE or run scripts/provision`. Neither build mode proves cluster connectivity or prevents a trusted caller from selecting a different configuration.

Protect credentials from direct agent access. If the configuration author must be separate from the agent, enforce a fixed invocation outside Toolbox. Read the [security model](security.md#configuration-trust).

## Windows

Use the [Windows configuration example](../config/toolbox.windows.example.json). Replace the example context and namespace list. Create the output directory before running this PowerShell command:

```powershell
go build -o "C:\Program Files\Toolbox\toolbox.exe" ./cmd/toolbox
& "C:\Program Files\Toolbox\toolbox.exe" --config "C:\ProgramData\toolbox\toolbox.json" kubectl get pods -n apps
```

Provision private Windows access-control lists for credential files and temporary directories. Unix permission modes do not configure those lists.

## Updates

| Update | Required action |
| --- | --- |
| Configuration passed with `--config` | Restart MCP. New CLI invocations load edits automatically. |
| Configured kubectl executable with `--config` | Restart MCP to accept the replacement. New CLI invocations hash it automatically. |
| Built-in default configuration | Rebuild with `scripts/provision`. Restart MCP. |
| kubectl executable used by the built-in default | Rebuild with `scripts/provision`. Restart MCP. |
| Token or certificate contents at approved paths | Restart MCP to reload credentials. New CLI calls reload automatically. |
| Expired token | Replace it through the operator's credential process. Toolbox cannot refresh it. |

If you need a new identity, review the [kubeconfig template](../config/reader.kubeconfig.example.yaml) and [namespaced RBAC example](../deploy/kubernetes/reader.yaml). Use the [optional cluster permissions](../deploy/kubernetes/cluster-reader.optional.yaml) only when required.

## Optional tunnels

Set `kubectl.allow_port_forward` to `true` to permit loopback tunnels. With `contexts`, set `allow_port_forward` on each approved context instead. Set `kubectl.port_forward_max_seconds` to the maximum lifetime. The default is 300 seconds. Restart MCP after configuration edits. Rebuild only when using the built-in default. Read [tunnel permissions and usage](raw-and-port-forward.md).
