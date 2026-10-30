# Client configuration

Complete [setup](setup.md) first. Replace `/opt/toolbox/bin/toolbox` with the absolute path to the provisioned executable. Toolbox uses the Model Context Protocol (MCP) over standard input/output. It does not expose an HTTP server.

## Claude Code

Register Toolbox for your user:

```sh
claude mcp add --transport stdio --scope user toolbox -- /opt/toolbox/bin/toolbox mcp
```

Inspect the entry with `claude mcp get toolbox`. Open `/mcp` inside Claude Code to check the connection. [Claude Code MCP reference](https://code.claude.com/docs/en/mcp).

For project configuration, add this entry to `.mcp.json` at the project root instead:

```json
{
  "mcpServers": {
    "toolbox": {
      "type": "stdio",
      "command": "/opt/toolbox/bin/toolbox",
      "args": ["mcp"]
    }
  }
}
```

Do not add credentials, `KUBECONFIG`, or `--config` to this entry.

Start an interactive request:

```sh
claude "Use toolbox to list pods in apps. Report their names and phases."
```

For a non-interactive request with the project configuration:

```sh
claude --mcp-config .mcp.json --strict-mcp-config --tools "" \
  --allowedTools "mcp__toolbox__kubectl_logs" \
  -p "Use toolbox to read the last 20 log lines from api-0 in apps over 10 minutes. Summarize errors."
```

`--tools ""` disables built-in tools. `--strict-mcp-config` selects the supplied MCP configuration. `--allowedTools` permits the named tool without an approval prompt. These settings do not create operating-system isolation. Claude's answer depends on the returned logs. No model-generated answer is asserted here. [CLI reference](https://code.claude.com/docs/en/cli-reference).

For Windows, register the native executable from PowerShell:

```powershell
claude mcp add --transport stdio --scope user toolbox -- "C:\Program Files\Toolbox\toolbox.exe" mcp
```

Use `"command": "C:\\Program Files\\Toolbox\\toolbox.exe"` in the JSON alternative.

## Codex

Register Toolbox:

```sh
codex mcp add toolbox -- /opt/toolbox/bin/toolbox mcp
```

Alternatively, add this entry to `~/.codex/config.toml`:

```toml
[mcp_servers.toolbox]
command = "/opt/toolbox/bin/toolbox"
args = ["mcp"]
```

Inspect the entry with `codex mcp list`. [Official OpenAI documentation](https://developers.openai.com/codex/mcp).

## Tools

| Tool | Required input | Optional input |
| --- | --- | --- |
| `kubectl_resources` | None | None |
| `kubectl_get` | `resource` | `name`, `namespace`, `selector` |
| `kubectl_logs` | `pod` | `namespace`, `container`, `previous`, `tail`, `since` |

Read [request and result examples](examples.md). Clients initialize the MCP session before calling tools. Running `toolbox mcp` alone waits for protocol input and prints no interactive prompt.

## Connection failures

| Result | Action |
| --- | --- |
| Configuration is not approved by a provisioned build | Run `scripts/provision` and register its output executable. |
| Configuration or executable integrity check fails | Restore the approved files or review the updates and rebuild. |
| Unsupported kubeconfig | Check the credential restrictions in [setup](setup.md). |
| Kubernetes returns Forbidden | Check the existing identity's permissions for that namespace and resource. |
| Read fails but the catalog succeeds | Check cluster connectivity and authorization. Catalog discovery does not contact Kubernetes. |

Client registration does not prevent direct shell or credential access. Apply the [deployment boundaries](security.md#deployment-boundaries) separately.
