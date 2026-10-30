# Toolbox

Toolbox gives artificial intelligence (AI) agents Kubernetes reads and optional port forwarding through a command-line interface (CLI) or the Model Context Protocol (MCP). We try to cleans up resource data, logs, and error output as much as possible to reduce the risk of leaks, but its not perfect right now so don't solelly rely on it blindly :)

## Install

Use Go 1.26.10 or newer:

```sh
go install github.com/globlit/toolbox@latest
# Or select a published version:
go install github.com/globlit/toolbox@v0.4.0
```

Or download a [release binary](https://github.com/globlit/toolbox/releases/latest) for Linux, macOS/Darwin, or Windows on ARM64 or x86-64. Releases include SHA-256 checksums and a Cosign signature bundle. Follow [installation and signature verification](docs/install.md) before running a downloaded binary.

Install kubectl separately. Follow [setup with an existing kubeconfig](docs/setup.md), then use `--config FILE`.

## Usage

Run the executable with your configuration. Replace `prod-a`, `apps`, and `api` with your context, namespace, and service names. Enable forwarding before starting a tunnel:

```sh
toolbox --config /etc/toolbox/toolbox.json kubectl get pods --context prod-a -A -o json
toolbox --config /etc/toolbox/toolbox.json kubectl port-forward service/api 18080:80 --context prod-a -n apps --duration 1m
toolbox --config /etc/toolbox/toolbox.json mcp
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

Toolbox allows multiple stuff such as catalog reads, approved raw GET paths, and finite pod logs. `toolbox` blocks "write" commands, Secrets, exec, proxy, and unknown endpoints. Optional port-forward tunnels can be used but there is no check of what's send to it so use it at your own risk (which is the same risk as running kubectl port-forward and allow the agent to interact with the endpoint anyway). Run `toolbox kubectl resources` to inspect the catalog.

ConfigMaps are enabled by default. Configure [ConfigMap exclusions and wildcard namespaces](docs/configmaps.md).

To further improve security, use a read-only Kubernetes identity; protect credentials from direct agent access Read the [security model](docs/security.md) before deployment.

## Reference

- [Installation, checksums, and Cosign verification](docs/install.md)
- [Setup and credential rotation](docs/setup.md)
- [Context selection](docs/contexts.md)
- [Port forwarding](docs/raw-and-port-forward.md#enable-tunnels)
- [CLI and MCP examples](docs/examples.md)
- [Client configuration](docs/clients.md)
- [Architecture and future glab integration](docs/architecture.md)
- [Dependency licenses](THIRD_PARTY_NOTICES.md)

License: [Apache-2.0](LICENSE).
