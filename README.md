# Toolbox

Toolbox gives artificial intelligence (AI) agents Kubernetes reads and optional port forwarding through a command-line interface (CLI) or the Model Context Protocol (MCP). It sanitizes resource data, logs, and error output. Native executables support Linux, macOS, and Windows.

## Setup

Use Go 1.26.10 or newer to build Toolbox. Install kubectl separately.

1. Follow [setup with an existing kubeconfig](docs/setup.md). Reuse the existing ServiceAccount, Role, and RoleBinding if they grant the required read-only access.
2. Configure the approved kubectl path, kubeconfig path, approved contexts, and namespaces.
3. Run the operator build from this checkout. Replace these paths with your approved locations. Create the output directory first.

```sh
go run ./scripts/provision -config /etc/toolbox/toolbox.json -out /opt/toolbox/bin/toolbox
```

Ordinary builds cannot execute requests. Runtime `--config` and `KUBECONFIG` overrides are unsupported. Rebuild after changing the Toolbox configuration or kubectl executable.

## Usage

Run the provisioned executable:

```sh
/opt/toolbox/bin/toolbox kubectl get pods --context apps-reader -A -o json
/opt/toolbox/bin/toolbox kubectl get --raw /api/v1/namespaces/apps/pods
/opt/toolbox/bin/toolbox kubectl logs api-0 -n apps --tail 20 --since 10m
```

Use `-A` for all approved namespaces. Read [context configuration](docs/setup.md#multiple-contexts) and [raw GET and port-forward usage](docs/raw-and-port-forward.md).

Read [six CLI and MCP examples with outputs](docs/examples.md). The examples cover pod lists, deployment status, sanitized logs, previous container logs, and denied Secret access.

## Claude Code

Register the provisioned executable with the [Claude Code CLI](https://code.claude.com/docs/en/mcp):

```sh
claude mcp add --transport stdio --scope user toolbox -- /opt/toolbox/bin/toolbox mcp
```

Read [client configuration](docs/clients.md) for project JSON, Windows paths, Claude prompts, and Codex registration.

## Restrictions

Toolbox permits catalog reads, approved raw GET paths, and finite pod logs. It rejects mutation commands, Secrets, ConfigMaps, exec, proxy, and unknown endpoints. Optional port-forward tunnels can carry writes and unsanitized traffic. Run `toolbox kubectl resources` to inspect the catalog.

Ambiguous log lines become `[WITHHELD: ambiguous log fragment]`. Independent neighboring records remain visible. Sanitization cannot identify every secret.

Use a read-only Kubernetes identity. Protect credentials from direct agent access. Same-user execution does not provide isolation. Read the [security model](docs/security.md) before deployment.

## Reference

- [Setup and credential rotation](docs/setup.md)
- [CLI and MCP examples](docs/examples.md)
- [Client configuration](docs/clients.md)
- [Architecture and future glab integration](docs/architecture.md)
- [Dependency licenses](THIRD_PARTY_NOTICES.md)

License: [Apache-2.0](LICENSE).
