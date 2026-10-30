# Client configuration

Complete [setup](setup.md) first. Replace `/opt/toolbox/bin/toolbox` with the absolute path to the executable. Replace `/etc/toolbox/toolbox.json` with your configuration path. Toolbox uses the Model Context Protocol (MCP) over standard input/output. It does not expose an HTTP server.

## Claude Code

Register Toolbox for your user:

```sh
claude mcp add --transport stdio --scope user toolbox -- /opt/toolbox/bin/toolbox --config /etc/toolbox/toolbox.json mcp
```

Inspect the entry with `claude mcp get toolbox`. Open `/mcp` inside Claude Code to check the connection. [Claude Code MCP reference](https://code.claude.com/docs/en/mcp).

For project configuration, add this entry to `.mcp.json` at the project root instead:

```json
{
  "mcpServers": {
    "toolbox": {
      "type": "stdio",
      "command": "/opt/toolbox/bin/toolbox",
      "args": ["--config", "/etc/toolbox/toolbox.json", "mcp"]
    }
  }
}
```

Use a trusted configuration file. Do not put credential contents or `KUBECONFIG` in the client entry. Omit `--config` only when using an operator-built default.

Start an interactive request:

```sh
claude "Use toolbox to list approved contexts, then list pods in apps on prod-a. Report their names and phases."
```

For a non-interactive request with the project configuration:

```sh
claude --mcp-config .mcp.json --strict-mcp-config --tools "" \
  --allowedTools "mcp__toolbox__kubectl_logs" \
  -p "Use toolbox to read the last 20 log lines from api-0 in apps on context prod-a over 10 minutes. Summarize errors."
```

`--tools ""` disables built-in tools. `--strict-mcp-config` selects the supplied MCP configuration. `--allowedTools` permits the named tool without an approval prompt. These settings do not create operating-system isolation. Claude's answer depends on the returned logs. No model-generated answer is asserted here. [CLI reference](https://code.claude.com/docs/en/cli-reference).

For Windows, register the native executable from PowerShell:

```powershell
claude mcp add --transport stdio --scope user toolbox -- "C:\Program Files\Toolbox\toolbox.exe" --config "C:\ProgramData\toolbox\toolbox.json" mcp
```

Use `"command": "C:\\Program Files\\Toolbox\\toolbox.exe"` and `"args": ["--config", "C:\\ProgramData\\toolbox\\toolbox.json", "mcp"]` in the JSON alternative.

## Codex

Register Toolbox:

```sh
codex mcp add toolbox -- /opt/toolbox/bin/toolbox --config /etc/toolbox/toolbox.json mcp
```

Alternatively, add this entry to `~/.codex/config.toml`:

```toml
[mcp_servers.toolbox]
command = "/opt/toolbox/bin/toolbox"
args = ["--config", "/etc/toolbox/toolbox.json", "mcp"]
```

Inspect the entry with `codex mcp list`. [Official OpenAI documentation](https://developers.openai.com/codex/mcp).

## Tools

| Tool | Required input | Optional input |
| --- | --- | --- |
| `kubectl_contexts` | None | None |
| `kubectl_resources` | None | `context` |
| `kubectl_get` | `resource` | `context`, `name`, `namespace`, `selector`, `all_namespaces` |
| `kubectl_logs` | `pod` | `context`, `namespace`, `container`, `previous`, `tail`, `since` |
| `kubectl_raw` | `path` | `context` |
| `kubectl_port_forward` | `target`, `ports` | `context`, `namespace`, `duration_seconds` |
| `kubectl_port_forward_status` | `id` | None |
| `kubectl_port_forward_stop` | `id` | None |
| `kubectl_port_forward_list` | None | None |

`context` is required when multiple contexts are configured. Read [context selection](contexts.md) and [raw GET and tunnel examples](raw-and-port-forward.md). Start and stop operations are not marked read-only.

Read [request and result examples](examples.md). Clients initialize the MCP session before calling tools. Running `toolbox --config /etc/toolbox/toolbox.json mcp` waits for protocol input and prints no interactive prompt.

## Connection failures

| Result | Action |
| --- | --- |
| `configuration required. Use --config FILE or run scripts/provision` | Add `--config` with an absolute file path to the registered command. |
| Default configuration integrity check fails | Restore the approved file or rebuild the default. |
| Executable integrity check fails | Verify the replacement. Restart MCP for `--config`, or rebuild the built-in default. |
| Unsupported kubeconfig | Check the credential restrictions in [setup](setup.md). |
| Kubernetes returns Forbidden | Check the existing identity's permissions for that namespace and resource. |
| Read fails but the catalog succeeds | Check cluster connectivity and authorization. Catalog discovery does not contact Kubernetes. |

Client registration does not prevent direct shell or credential access. Apply the [deployment boundaries](security.md#deployment-boundaries) separately.
