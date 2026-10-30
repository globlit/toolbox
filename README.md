# Toolbox

Toolbox gives artificial intelligence (AI) agents Kubernetes reads and optional port forwarding through a command-line interface (CLI) or the Model Context Protocol (MCP). It sanitizes resource data, logs, and error output. Native executables support Linux, macOS, and Windows.

## Install

Use Go 1.26.10 or newer:

```sh
go install github.com/globlit/toolbox@latest
# Or select a published version:
go install github.com/globlit/toolbox@v0.4.0
```

Or download a [release binary](https://github.com/globlit/toolbox/releases/latest) for Linux, macOS/Darwin, or Windows on ARM64 or x86-64. Releases include SHA-256 checksums and a Cosign signature bundle. Follow [installation and signature verification](docs/install.md) before running a downloaded binary.

Public installation requires the first release from the new workflow. Install kubectl separately. Follow [setup with an existing kubeconfig](docs/setup.md), then pass `--config FILE`. Toolbox trusts the configuration author. Environment variables cannot select configuration.

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

Toolbox permits catalog reads, approved raw GET paths, and finite pod logs. It rejects mutation commands, Secrets, ConfigMaps, exec, proxy, and unknown endpoints. Optional port-forward tunnels can carry writes and unsanitized traffic. Run `toolbox kubectl resources` to inspect the catalog.

Ambiguous log lines become `[WITHHELD: ambiguous log fragment]`. Independent neighboring records remain visible. Sanitization cannot identify every secret.

Use a read-only Kubernetes identity. Protect credentials from direct agent access. Same-user execution does not provide isolation. Read the [security model](docs/security.md) before deployment.

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
