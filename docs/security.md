# Security model

Toolbox restricts its command-line interface (CLI) and Model Context Protocol (MCP) server to approved reads and optional port-forward tunnels. Sanitization reduces disclosure but cannot identify every secret.

## Deployment boundaries

Apply three controls:

1. **Request restrictions.** Toolbox rejects mutation commands, unsupported resources, arbitrary flags, and multiple-resource requests. Raw GET paths must match approved resource or discovery endpoints. Other subresources remain blocked. The catalog fixes API groups and versions. Missing API versions fail without fallback.
2. **Cluster permissions.** Use a dedicated read-only identity. Kubernetes role-based access control (RBAC) grants are additive. Inspect all bindings affecting that identity. The reader manifests exclude Secrets, ConfigMaps, mutation, exec, attach, proxy, port-forward, token creation, and impersonation. The separate optional tunnel role grants only `get` and `create` on `pods/portforward`. [RBAC reference](https://kubernetes.io/docs/reference/access-authn-authz/rbac/).
3. **Credential isolation.** Run the gateway under a separate account or container if the agent must not access raw credentials or outputs. Expose a fixed invocation over standard input/output. Block direct cluster access. Same-user execution does not enforce this isolation.

Protect Toolbox, kubectl, configuration, credentials, dependencies, and their parent directories from agent modification. The approved configuration path and configuration/kubectl digests are compiled into the executable. Caller flags and environment variables cannot replace them. Configuration and kubectl updates require a new operator build.

Toolbox assumes a trusted operating system, kubectl executable, and Kubernetes API server. API GET endpoints must obey Kubernetes read semantics. Toolbox cannot sandbox a compromised executable or malicious API extension. An agent able to replace the gateway or access privileged credentials directly can bypass this boundary.

## Port forwarding

Enable tunnels only when direct access to pod services is acceptable. Tunnel traffic can mutate services and expose secrets. Toolbox does not inspect or sanitize that traffic. Other local processes can connect to the loopback listener.

Tunnels bind to `127.0.0.1`. They share the two-process limit with reads. Expiration, explicit stop, or normal gateway shutdown closes the subprocess. Forced gateway termination may leave a child process until its operating system terminates it. A separate supervisor or container must enforce cleanup after forced termination.

Read [configuration and permissions](raw-and-port-forward.md).

## Credentials

Use one YAML or JSON `v1 Config` document. The loader accepts a static bearer token or client certificate/key pair. Certificate files resolve relative to the kubeconfig location when their paths are relative.

The server address must be a direct HTTPS origin. System certificate trust applies when no certificate authority is supplied. The loader rejects exec plugins, authentication providers, impersonation, proxies, insecure TLS options, token files, extensions, unknown fields, duplicate names, and conflicting credential sources. Unknown-field validation includes unused entries.

Each source configuration or credential file has a 1 MiB limit. The loader snapshots all approved contexts at startup. Each request selects an approved context and its namespace restrictions. Unknown contexts fail before execution. Multiple configured contexts require explicit selection. kubectl receives a reconstructed snapshot, not the source kubeconfig. Source-file edits cannot alter an existing MCP process's credentials. Restart MCP after credential rotation. Expired credentials cause a failed request without identity fallback.

Read [existing-kubeconfig setup](setup.md) for the accepted fields. Accept configuration only from a trusted operator. [Kubernetes kubeconfig guidance](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/).

## Sanitization

Resource reads require valid JSON objects. The sanitizer recursively replaces sensitive fields, including environment arrays, annotations, command arguments, request/response bodies, SQL fields, and credential objects. It also processes keys and embedded strings. Objects deeper than 64 levels are  redacted and replaced with a marker instead.

Log processing checks sensitive labels and continuation context before parsing JSON suffixes. Ambiguous physical lines become `[WITHHELD: ambiguous log fragment]`. Newline positions and independent neighboring records remain. Ambiguous inputs include orphaned indentation, bare values, unframed plaintext, malformed structured fragments, and Python dictionary representations.

Configured literal and regular-expression rules apply to keys, strings, numbers, booleans, nulls, and multiline matches. Numeric matching checks bounded equivalent exponent representations. Oversized numeric representations are redacted when configured rules require classification. Cross-line matches replace only intersecting lines. The selected token and encoded certificate/key strings are added as literals.

Secrets can survive when they appear as unlabeled sentences/wording, unfamiliar encodings, URL path segments, confusable Unicode, or ordinary-looking records separated from their sensitivity context by `tail` or `since`. Short secrets, personal data, and application-specific formats may also escape detection. Do not treat Toolbox as a complete secure prevention system. Suppress secrets at the source and add approved application fixtures and explicit rules.

Failed commands return sanitized standard error and a generic error. Their partial standard output is discarded. Malformed resource JSON, overflow, timeout, cancellation, or an oversized serialized result suppresses the whole response. `kubectl --limit-bytes` is not used because it can cut a credential mid-record. No raw-output switch exists.

Sanitization does not prevent prompt injection. MCP output is marked untrusted. Clients must treat log instructions as data. Toolbox's request restrictions apply regardless of those instructions.

## Limits

| Limit | Default | Maximum |
| --- | --- | --- |
| Request duration | 30 seconds | 120 seconds |
| Combined subprocess output | 1 MiB | 8 MiB |
| Concurrent subprocesses per service | 2 | 2 |
| Log tail | 200 lines | 2,000 lines |
| Log lookback | 1 hour | 24 hours |
| Tunnel lifetime | 300 seconds | 3,600 seconds |

All contexts and tunnels share the two-process limit. `-A` queries approved namespaces separately. It never requests a cluster-wide namespace list. Combined namespace results share the configured timeout and output budget. The serialized result must also fit the configured output limit. Narrow selectors or reduce the log tail when a response exceeds that limit.

The synthetic fixture corpus covers OpenSearch, RabbitMQ, Kvrocks, CNPG/Postgres, Python, cert-manager, external-dns, Prometheus/Alertmanager, Fluent Bit, Cilium, CoreDNS, and generic workloads. `kubeseak` is treated as a workload label. These fixtures do not establish production format coverage.
