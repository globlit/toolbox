# Toolbox

A local, read-only gateway between an AI agent and operational tools. The first integration is **kubectl**, exposed through a CLI and an MCP stdio server. Go produces a native executable for macOS, Linux, and Windows; the runtime needs kubectl but no Go, Node, Python, hosted service, or paid dependency.

**Write operations are absent from the tool API.** Requests select typed reads; they cannot supply commands, flags, raw API paths, plugins, credentials, or contexts. The gateway also sanitizes resource output, logs, and stderr before returning them.

**Security boundary:** run with a dedicated read-only Kubernetes identity. An agent with unrestricted shell access and administrator credentials can bypass any wrapper. Keep privileged credentials, the trusted binary, and operator configuration outside the agent's control. See [the threat model](docs/security.md) before deploying. Sanitization is best effort, not a guarantee that arbitrary logs contain no secrets.

## Build and run

Go 1.26.10 or newer is needed only for the operator build. Version 0.2 removes runtime `--config`: the trusted executable embeds the approved configuration path, its SHA-256 digest, and the kubectl executable digest. There is no environment, home-directory, or current-directory fallback.

An ordinary `go build ./cmd/toolbox` produces an **unprovisioned** binary: help/version work, but operations fail closed. `go run ./scripts/build` produces six such cross-platform binaries and checksums for build verification. Provision a usable binary as follows:

1. An operator reviews and provisions [the RBAC example](deploy/kubernetes/reader.yaml). Its RoleBinding grants access only in `apps`; repeat that binding only in approved namespaces. Existing namespaces are required. No deployment commands run automatically.
2. Provision a dedicated, preferably short-lived bearer token or client certificate/key pair with these permissions. Never use an administrator kubeconfig. Fill out [the kubeconfig template](config/reader.kubeconfig.example.yaml) outside this repository.
3. Copy [the configuration example](config/toolbox.example.json) or [Windows example](config/toolbox.windows.example.json) to an operator-controlled location. Set the absolute binary/kubeconfig paths, context, and namespaces. Do not blindly approve the example's namespaces.
4. From this source checkout, run the separate **operator-only** provisioning command. It reads and hashes the approved configuration and kubectl, then builds the gateway. This command is not included in the runtime tool API:

```sh
go run ./scripts/provision -config /etc/toolbox/toolbox.json -out /opt/toolbox/bin/toolbox
```

PowerShell equivalent, with reviewed native paths:

```powershell
go run ./scripts/provision -config "C:\ProgramData\toolbox\toolbox.json" -out "C:\Program Files\Toolbox\toolbox.exe"
```

The output directory must already exist and be operator-controlled. Protect the installed gateway, kubectl, configuration, credentials, and their directories from agent modification. Use the provisioned executable directly; do not delegate the build command to the agent. Configuration or kubectl updates require reprovisioning. Credential contents may rotate at the approved kubeconfig path; restart MCP to refresh its snapshot.

The runtime commands are:

```sh
toolbox kubectl resources
toolbox kubectl get pods -n apps
toolbox kubectl get deployments api -n apps
toolbox kubectl get events -n apps
toolbox kubectl get clusters.postgresql.cnpg.io -n apps
toolbox kubectl logs api-0 -n apps -c app --tail 200 --since 30m
toolbox kubectl logs api-0 --previous
```

Flags may appear before or after the resource/pod. Supported flags are exactly those shown in `--help`; there is no argument passthrough. Exit codes: `0` success, `1` denied/failed request or transport failure, `2` invalid CLI/configuration. Output is a JSON envelope with `data` or `text`, optional sanitized `diagnostics`, a redaction count, optional `withheld_lines`, and `untrusted: true`. A failed kubectl request has an `error`; no partial stdout is returned. An empty successful log response may omit `text`.

## MCP clients

Launch `toolbox mcp`. The server uses the official Go MCP SDK, negotiates protocol versions, and exposes:

| Tool | Input | Result |
| --- | --- | --- |
| `kubectl_resources` | `{}` | Available resource types, API versions, and namespaces |
| `kubectl_get` | `resource`, optional `name`, `namespace`, `selector` | Sanitized resource JSON |
| `kubectl_logs` | `pod`, optional `namespace`, `container`, `previous`, `tail`, `since` | Sanitized finite log snapshot |

Codex registration, using its [documented MCP configuration](https://developers.openai.com/codex/mcp):

```sh
codex mcp add toolbox -- /absolute/path/toolbox mcp
```

Equivalent TOML:

```toml
[mcp_servers.toolbox]
command = "/absolute/path/toolbox"
args = ["mcp"]
```

[Claude Code](https://code.claude.com/docs/en/mcp) registration:

```sh
claude mcp add --transport stdio toolbox -- /absolute/path/toolbox mcp
```

For Windows, use native `.exe` paths and quote paths containing spaces. Other clients accepting an `mcpServers` object can use:

```json
{
  "mcpServers": {
    "toolbox": {
      "command": "C:\\Program Files\\Toolbox\\toolbox.exe",
      "args": ["mcp"]
    }
  }
}
```

These are registration examples, not sandbox configuration. Same-user local MCP execution does not make its credential files inaccessible to that user's other programs. For enforced sanitization, isolate the gateway under a separate account/container and give the agent only a fixed invocation/stdio channel; block direct credential and cluster access.

## Read surface and sanitization

The catalog covers ordinary workloads, services, events, storage claims, ingress/network policy, CNPG, the RabbitMQ and OpenSearch operators, cert-manager, Prometheus Operator, and Cilium. Unknown CRDs are denied. API versions are pinned in the catalog; unavailable versions fail without fallback. List reads are bounded whole responses; narrow large lists with label selectors. Cluster-scoped reads are disabled unless explicitly enabled and separately granted with [optional RBAC](deploy/kubernetes/cluster-reader.optional.yaml). Kvrocks, Python services, Fluent Bit, CoreDNS, external-dns, and other workloads use ordinary pod reads/logs; log sanitization does not depend on container names. `kubeseak` is treated as a workload label, not an assumed product-specific parser.

Secrets and ConfigMaps cannot be read. There is no `describe`, `top`, watch/follow, exec, attach, debug, proxy, port-forward, cp, arbitrary API, shell, plugin, template/file output, impersonation, context switching, or write mode. Use structured `get` and events for diagnosis; metrics can be a future typed integration.

Sanitization walks structured/nested JSON and recognizes timestamped, severity-prefixed, and logfmt records. Unsupported Python repr, malformed structured fragments, orphaned continuations, bare values, and unframed plaintext are withheld per physical line with `[WITHHELD: ambiguous log fragment]`. It does not evaluate log content. It removes sensitive fields, environment values, annotations, command arguments, request/response bodies, SQL fields, credentials, common tokens, URL credentials/query values, emails, IP addresses, and long token-like strings. Sensitive labels are checked before JSON suffixes. Quote/PEM continuations remain hidden until their closing delimiter; an unterminated sensitive span is withheld through the end, while preceding records remain. Complete spans preserve the independent records before and after them. Configured literal/pattern rules cover strings, numeric/boolean/null scalars, and multiline matches. This is deliberately conservative and can hide harmless lines. [Details and limitations](docs/security.md#sanitization).

Operator-supplied `redaction.literals` and Go/RE2 `redaction.patterns` add application-specific rules. There is no agent-controlled bypass. Default limits: 30 seconds, 1 MiB combined subprocess output, two concurrent processes, 200 log lines, and a one-hour log window. Maximums: 120 seconds, 8 MiB, 2,000 lines, 24 hours. Oversized or interrupted output is withheld in full; malformed resource JSON never falls back to plain text.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

Opt in to an integration test against an ephemeral **local mock API**, using a real installed kubectl:

```sh
TOOLBOX_TEST_KUBECTL=/absolute/path/kubectl go test -v ./internal/integrations/kubectl -run 'TestRealKubectl|TestSecurityRegressionsOverCLIAndMCP'
```

PowerShell: set `$env:TOOLBOX_TEST_KUBECTL` before the same `go test` command. This test does not use your current context or cluster. Synthetic log fixtures and injected canaries exercise the listed service formats; they are not a claim of validation against your actual logs. Native OS tests are defined in CI; cross-compilation alone does not prove native runtime behavior.

See [architecture and adding glab](docs/architecture.md), [validation evidence](docs/validation.md), [security re-audit](docs/security-audit.md), and [dependency licenses](THIRD_PARTY_NOTICES.md). Project license: Apache-2.0.
