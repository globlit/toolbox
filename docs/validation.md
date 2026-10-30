# Validation

## Security validation — 2026-10-29

Verification used Linux amd64, Go 1.26.10, and Model Context Protocol (MCP) Go SDK v1.8.0. kubectl v1.34.1 contacted a temporary local mock API with Transport Layer Security (TLS). Credentials and log data were synthetic. No production cluster or existing kubeconfig was accessed.

| Check | Result |
| --- | --- |
| Race-enabled package tests | Passed, including provisioned command-line interface (CLI), MCP subprocesses, and real-kubectl mock tests |
| MCP protocol revisions | Discovery and calls passed for `2025-06-18`, `2025-11-25`, and `2026-07-28` |
| Policy and permission manifests | Passed fixed-route, denial, namespace, API-group collision, and role-based access control (RBAC) checks |
| Process and output restrictions | Passed credential snapshot, environment, concurrency, cleanup, cancellation, overflow, and malformed-output checks |
| Disclosure regressions | Passed F1–F5 and D1 checks listed in the [audit report](security-audit.md) |
| Synthetic log corpus | Preserved diagnostic markers and removed injected secret markers across the covered service formats |
| Five-second, single-worker fuzz runs | Passed `FuzzSensitiveSuffix` (31,436 executions), `FuzzSensitiveJSON` (21,063), and `FuzzPolicy` (37,868) |
| Static checks | `go vet ./...` and `go mod verify` passed |
| Dependency scan | `govulncheck v1.9.0` reported `No vulnerabilities found.` |
| Platform builds | Passed Linux, macOS, and Windows builds for amd64 and arm64 |

Build checks verified embedded Go/OS/architecture metadata and every `dist/SHA256SUMS` entry. Only Linux amd64 ran locally. The six portable builds are unprovisioned. Version output succeeds, while operational requests fail. Subprocess tests exercise operator-provisioned builds.

Fuzz runs provide bounded coverage. The dependency result reflects the vulnerability database available on the validation date.

## Reproduce checks

Run from the source checkout:

```sh
go test -race ./...
go vet ./...
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go run ./scripts/build
```

Replace `/absolute/path/kubectl` with a native kubectl executable to include local mock API tests:

```sh
TOOLBOX_TEST_KUBECTL=/absolute/path/kubectl GOMAXPROCS=2 go test -race -p=1 ./...
```

On PowerShell, set `$env:TOOLBOX_TEST_KUBECTL` before running `go test -race ./...`.

Run targeted wire and policy checks:

```sh
TOOLBOX_TEST_KUBECTL=/absolute/path/kubectl go test -count=1 -v ./internal/integrations/kubectl -run 'TestAuditPolicyInvariants|TestRealKubectl|TestSecurityRegressionsOverCLIAndMCP'
```

Inspect local generated evidence under `dist/audit/`: `race-tests.txt`, `wire-and-policy.txt`, `fuzz-suffix.txt`, `fuzz-json.txt`, `fuzz-policy.txt`, `vulnerabilities.txt`, `builds.txt`, and `build-verification.txt`. Git ignores these files.

## Documentation verification — 2026-10-30

The six [usage examples](examples.md) ran through a provisioned CLI or an MCP client using a local TLS mock API. The mock returned synthetic resources and logs. Successful examples produced sanitized output. The Secret request was rejected before kubectl execution. JSON blocks match the captured results after formatting. Local captures are in `dist/docs/example-results.json`.

Client command syntax was checked against local CLI help and official documentation. No Claude model request ran. No client registration was changed. Existing-kubeconfig permission checks are operator instructions, not observations from a real cluster.

## Deployment checks

Verify these boundaries before production use:

- Native macOS/Windows execution, filesystem access controls, cancellation, and client integration. Continuous integration defines native tests, but no remote run was observed here.
- Actual cluster permissions, all grants affecting the identity, API availability, and network isolation.
- Prevention of direct agent access to credentials or unsanitized outputs.
- Representative approved production logs and application-specific redaction rules.
- A supported kubectl executable. Toolbox verifies its approved digest but does not establish provenance or install updates.

Toolbox does not apply permission manifests or register clients during installation.
