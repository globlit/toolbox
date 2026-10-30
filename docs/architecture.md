# Architecture

Toolbox exposes the same Kubernetes service through its command-line interface (CLI) and Model Context Protocol (MCP) server.

```text
cmd/toolbox/                    process entry point
internal/cli/                   CLI parsing and JSON output
internal/transport/mcp/         MCP schemas and handlers
internal/integrations/kubectl/  catalog, request policy, credential loading
internal/runner/                subprocess limits and isolation
internal/sanitize/              output redaction and synthetic fixtures
internal/config/                approved configuration checks
internal/provision/             operator build package
internal/testutil/              test fixtures
config/                        operator templates
deploy/kubernetes/             permission examples
scripts/build/                 six unprovisioned platform builds
scripts/provision/             operator build command
docs/                          setup, examples, security, validation
```

## Requests

Both transports call `kubectl.Service`. The service validates each request against a fixed resource catalog. It generates an exact, versioned Kubernetes API path for `kubectl get --raw`. Callers cannot supply raw paths or additional flags. Fixed API groups prevent discovery from selecting a different resource with the same name.

The runner executes an absolute kubectl path without a shell. The service sanitizes the result before returning it to either transport. MCP read-only annotations describe these restrictions but do not enforce them.

## Configuration

The operator build records the approved configuration path and SHA-256 digests for configuration and kubectl. The runtime verifies configuration bytes before parsing them. The runner verifies kubectl before execution. An unprovisioned build rejects operational requests.

The provisioning package is absent from the runtime dependency graph. Agent requests cannot select configuration, credentials, contexts, or executables.

The credential loader converts the selected context into a minimal snapshot with static credentials. Each subprocess receives a private temporary kubeconfig. Toolbox writes no persistent raw-output logs. Read [security](security.md) for filesystem and process-isolation limits.

## Future glab integration

Add typed GitLab reads under `internal/integrations/glab/`. Define separate operations for merge requests, pipelines, and job logs. Add CLI and MCP adapters for those operations.

Reuse the sanitizer and runner. Provide GitLab-specific credential isolation and an endpoint/method allowlist. Do not expose arbitrary shell commands, REST requests, or GraphQL documents. GraphQL documents can request mutations.

Require denial tests, redaction fixtures, and read-scoped credentials for each integration. No glab execution is implemented.

The module name is `toolbox`. Update the module declaration and internal imports together when selecting a repository URL.
