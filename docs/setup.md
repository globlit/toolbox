# Setup

Use an existing read-only kubeconfig to build a provisioned Toolbox executable. Toolbox does not create Kubernetes identities or apply permission manifests.

## Prerequisites

- Go 1.26.10 or newer on the build host.
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

The kubeconfig credential determines the Kubernetes identity. Its `users[].name` is a local reference, not a service-account assignment. The selected context links that reference to a cluster. Toolbox uses the configured context and namespace list rather than the kubeconfig's current context or default namespace.

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

Set absolute file paths. Include `default_namespace` in `namespaces`. Namespace entries restrict Toolbox requests but grant no Kubernetes permissions. Leave cluster-scoped access disabled unless the operator approves it and provides the corresponding permissions.

## 4. Build and verify

Create the output directory with operator-controlled permissions. Run this command from the source checkout:

```sh
go run ./scripts/provision -config /etc/toolbox/toolbox.json -out /opt/toolbox/bin/toolbox
```

Expected output:

```text
Gateway build succeeded.
```

The build records the approved configuration path and SHA-256 digests. It does not test cluster connectivity. Verify a permitted read:

```sh
/opt/toolbox/bin/toolbox kubectl get pods -n apps
```

Expect a JSON result with `data`, `redactions`, and `untrusted`. An empty namespace returns an empty `items` array. If the command fails, check the selected context, file access, credential validity, certificate trust, and resource permission. `toolbox kubectl resources` lists Toolbox's catalog without checking cluster permissions.

Register the executable using [client configuration](clients.md). Protect the executable and its parent directories from agent modification. Apply the same protection to kubectl, configuration, and credentials. Use separate operating-system isolation if the agent must not read credentials or contact Kubernetes directly.

## Windows

Use the [Windows configuration example](../config/toolbox.windows.example.json). Replace the example context and namespace list. Create the output directory before running this PowerShell command:

```powershell
go run ./scripts/provision -config "C:\ProgramData\toolbox\toolbox.json" -out "C:\Program Files\Toolbox\toolbox.exe"
& "C:\Program Files\Toolbox\toolbox.exe" kubectl get pods -n apps
```

Provision private Windows access-control lists for credential files and temporary directories. Unix permission modes do not configure those lists.

## Updates

| Update | Required action |
| --- | --- |
| Toolbox configuration, including paths or redaction rules | Rebuild with `scripts/provision`. Restart MCP. |
| Approved kubectl executable | Rebuild with `scripts/provision`. Restart MCP. |
| Token or certificate contents at approved paths | Restart MCP to reload credentials. New CLI calls reload automatically. |
| Expired token | Replace it through the operator's credential process. Toolbox cannot refresh it. |

If you need a new identity, review the [kubeconfig template](../config/reader.kubeconfig.example.yaml) and [namespaced RBAC example](../deploy/kubernetes/reader.yaml). Use the [optional cluster permissions](../deploy/kubernetes/cluster-reader.optional.yaml) only when required.

## Optional tunnels

Set `kubectl.allow_port_forward` to `true` to permit loopback tunnels. Set `kubectl.port_forward_max_seconds` to the maximum lifetime. The default is 300 seconds. Rebuild the gateway after configuration edits. Read [tunnel permissions and usage](raw-and-port-forward.md).
