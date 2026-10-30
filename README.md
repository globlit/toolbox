# Toolbox

Toolbox gives artificial intelligence (AI) agents Kubernetes reads and optional port forwarding through a command-line interface (CLI) or the Model Context Protocol (MCP). It sanitizes resource data, logs, and error output. Native executables support Linux, macOS, and Windows.

## Setup

Use Go 1.26.10 or newer to build Toolbox. Install kubectl separately.

1. Follow [setup with an existing kubeconfig](docs/setup.md). Reuse the existing ServiceAccount, Role, and RoleBinding when they provide the required access.
2. Configure the kubectl path, kubeconfig path, contexts, and namespaces.
3. Build Toolbox from this checkout:

```sh
go build -o ./toolbox ./cmd/toolbox
```

Pass the configuration with `--config FILE`. Toolbox trusts that file's author. Environment variables cannot select configuration. [Operator builds](docs/setup.md#optional-built-in-default) can provide a default configuration.

## Usage

Run the executable with your configuration. Replace `prod-a`, `apps`, and `api` with your context, namespace, and service names. Enable forwarding before starting a tunnel:

```sh
./toolbox --config /etc/toolbox/toolbox.json kubectl get pods --context prod-a -A -o json
./toolbox --config /etc/toolbox/toolbox.json kubectl port-forward service/api 18080:80 --context prod-a -n apps --duration 1m
./toolbox --config /etc/toolbox/toolbox.json mcp
```

Use `-A` for all approved namespaces. Read [context selection](docs/contexts.md) and [raw GET and port-forward usage](docs/raw-and-port-forward.md).

Read [six CLI and MCP examples with outputs](docs/examples.md). The examples cover pod lists, deployment status, sanitized logs, previous container logs, and denied Secret access.

## Claude Code

Register the executable with the [Claude Code CLI](https://code.claude.com/docs/en/mcp):

```sh
claude mcp add --transport stdio --scope user toolbox -- /opt/toolbox/bin/toolbox --config /etc/toolbox/toolbox.json mcp
```

Read [client configuration](docs/clients.md) for project JSON, Windows paths, Claude prompts, and Codex registration.

## Restrictions

Toolbox permits catalog reads, approved raw GET paths, and finite pod logs. It rejects mutation commands, Secrets, ConfigMaps, exec, proxy, and unknown endpoints. Optional port-forward tunnels can carry writes and unsanitized traffic. Run `toolbox kubectl resources` to inspect the catalog.

Ambiguous log lines become `[WITHHELD: ambiguous log fragment]`. Independent neighboring records remain visible. Sanitization cannot identify every secret.

Use a read-only Kubernetes identity. Protect credentials from direct agent access. Same-user execution does not provide isolation. Read the [security model](docs/security.md) before deployment.

## Reference

- [Setup and credential rotation](docs/setup.md)
- [Context selection](docs/contexts.md)
- [Port forwarding](docs/raw-and-port-forward.md#enable-tunnels)
- [CLI and MCP examples](docs/examples.md)
- [Client configuration](docs/clients.md)
- [Architecture and future glab integration](docs/architecture.md)
- [Dependency licenses](THIRD_PARTY_NOTICES.md)

License: [Apache-2.0](LICENSE).
