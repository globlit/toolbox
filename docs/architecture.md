# Architecture

```text
cmd/toolbox/                    process entry point, signals, stdio smoke test
internal/cli/                   strict CLI grammar and JSON rendering
internal/transport/mcp/         official MCP SDK adapter, schemas, annotations
internal/integrations/kubectl/  typed requests, closed catalog, policy, credential snapshot
internal/runner/                isolated subprocess execution, cancellation, output bounds
internal/sanitize/              mandatory shared redaction, synthetic service fixtures
internal/config/                compiled trust anchors, strict configuration and hard limits
internal/provision/             operator-only sealed build tooling; not linked into gateway
internal/testutil/              subprocess fixture; imported only by tests
config/                        nonfunctional operator templates
deploy/kubernetes/             least-privilege RBAC examples
scripts/build/                 sequential unprovisioned cross-platform builds and checksums
scripts/provision/             host-native operator build with pinned trust anchors
docs/                          security, architecture, validation
```

Both transports call `kubectl.Service`. The integration validates every field, resolves a resource from the static catalog, and constructs an exact versioned API GET path and an argument vector. Internally it uses `kubectl get --raw` for that fixed path; agents cannot provide a raw path. This prevents discovery from selecting another API group with the same resource name. The private runner invokes an absolute trusted executable directly, without a shell. The service sanitizes all returned data before either transport sees it. MCP read-only annotations describe behavior; they do not enforce it.

Configuration is operator authority. The runtime has no config-selection flag or environment override. `internal/config` verifies the exact approved configuration bytes against a compiled digest before parsing; `internal/runner` verifies the compiled kubectl digest and native header at construction and before execution. A plain build is unprovisioned and refuses requests. The operator-only provisioner is a separate Go program and is not linked into the gateway. Agent inputs cannot add catalog entries, override limits, choose credentials, disable redaction, or alter the executable. A selected kubeconfig context is reconstructed with fixed names and static inline credentials. Relative certificate paths are resolved by the operator loader and copied into the snapshot. Every invocation uses a private temporary working directory and discovery cache; no persistent raw output or audit payload is written. Credentials necessarily exist briefly in a private temporary file used by kubectl.

## Future integrations

Add GitLab under `internal/integrations/glab/`, with separate typed operations such as merge-request read, pipeline read, and job-log read. Add corresponding CLI/MCP adapters. Reuse the sanitizer and bounded runner with an integration-owned setup function, but design GitLab-specific identity/configuration isolation: kubectl's kubeconfig snapshot is not a generic credential solution.

Do not expose a generic `run`, `api`, shell, GraphQL, or REST passthrough. A GitLab read adapter needs an explicit endpoint/method allowlist and a read-scoped token; an arbitrary GraphQL document can contain mutations. Treat every newly supported endpoint as a policy change with denial tests, sanitized-output fixtures, and least-privilege credentials. No glab execution is implemented yet.

The Go module is currently named `toolbox` because no hosting location was supplied. Change it and internal import paths together when choosing a repository URL.
