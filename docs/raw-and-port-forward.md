# Raw GET and port forwarding

Add `--config /etc/toolbox/toolbox.json` to each CLI command below, or use an operator-built default. Configure the same file in your [MCP client](clients.md).

## Raw GET

Read a resource by its API path:

```sh
toolbox kubectl get --raw /api/v1/namespaces/apps/pods
toolbox kubectl get --raw='/apis/apps/v1/namespaces/apps/deployments?labelSelector=app%3Dapi&limit=20'
toolbox kubectl get --raw /version
```

MCP tool call:

```json
{"name":"kubectl_raw","arguments":{"path":"/api/v1/namespaces/apps/pods"}}
```

Raw requests permit GET only. The path must match a catalog resource in an approved namespace, an enabled cluster-scoped resource, or a discovery endpoint. Discovery supports `/version`, `/api`, `/apis`, and catalog API groups/versions. Resource queries accept `labelSelector`, `fieldSelector`, and a list `limit` from 1 to 2,000.

Exec, attach, proxy, Secrets, watch, unknown endpoints, encoded path segments, and duplicate query keys are rejected. Use `kubectl_logs` or `toolbox kubectl logs` for bounded log reads. Raw requests accept `--context` and `-o json`. They reject resource names, namespace flags, selectors, and `-A`. Returned JSON receives the same sanitization as typed reads. [`kubectl get` reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_get/).

With `namespaces: ["*"]`, approved collection paths such as `/api/v1/pods` can list across namespaces. ConfigMap paths enforce [ConfigMap exclusions](configmaps.md). A cluster-wide ConfigMap path is rejected when exclusions require separate reads.

## Enable tunnels

Add these fields to the approved configuration's `kubectl` object:

```json
{
  "allow_port_forward": true,
  "port_forward_max_seconds": 300
}
```

With `kubectl.contexts`, set `allow_port_forward` per context. Keep `port_forward_max_seconds` in the `kubectl` object. Restart MCP after edits. Rebuild with `scripts/provision` only for a built-in default. Forwarding is disabled by default. The lifetime limit accepts 1 to 3,600 seconds.

Grant the existing identity `get` and `create` on `pods/portforward` in each approved namespace. Review the [optional role and binding](../deploy/kubernetes/port-forward.optional.yaml). Existing pod, service, and deployment reads remain necessary for target resolution. The reader role is unchanged.

Tunnel traffic can perform writes and disclose secrets. Toolbox does not sanitize it. Kubernetes authorizes tunnel establishment, not individual requests through that tunnel.

## CLI

Replace `prod-a`, `api-0`, `api`, and `apps` with your approved context, pod, service, and namespace names:

```sh
toolbox kubectl port-forward api-0 18080:8080 --context prod-a -n apps --duration 5m
toolbox kubectl port-forward service/api :80 --context prod-a -n apps --duration 1m
toolbox kubectl port-forward deployment/api 18080:8080 19090:9090 --context prod-a -n apps
```

Use `--context NAME` or the MCP `context` field when multiple contexts are configured. Tunnel results identify the selected context.

Illustrative first result for `service/api :80`:

```json
{
  "context": "prod-a",
  "id": "EXAMPLE_TUNNEL_ID",
  "target": "services/api",
  "namespace": "apps",
  "address": "127.0.0.1",
  "ports": [{"local":49152,"remote":80}],
  "expires_at": "2026-10-30T12:01:00Z",
  "status": "active",
  "traffic_sanitized": false,
  "read_only": false
}
```

Connect your local client to the returned address and local port while the Toolbox command runs. The ID, random port, and expiry vary per invocation.

The first result reports `status: "active"`, the tunnel `id`, and assigned ports. The command stays running. Press Ctrl+C to stop it. A final result reports `stopped`, `expired`, or `closed`. Startup errors return a generic diagnostic without raw subprocess output.

Targets support pod names or `pod/`, `service/`, and `deployment/` prefixes. Ports use numeric `[LOCAL:]REMOTE` mappings. Supply one to eight mappings. `:REMOTE` or `0:REMOTE` requests a random local port. Fixed local ports must be 1024–65535. Remote ports must be 1–65535. Named ports are unsupported.

Every tunnel binds to `127.0.0.1`. Arbitrary bind addresses are rejected. The default lifetime is the operator limit. This interface supports a restricted subset of [`kubectl port-forward`](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_port-forward/).

## MCP

Start a tunnel:

```json
{"name":"kubectl_port_forward","arguments":{"context":"prod-a","target":"service/api","ports":[":80"],"namespace":"apps","duration_seconds":60}}
```

The start call returns the same fields as the CLI result in `result.structuredContent`. It returns after the tunnel becomes active. The tunnel continues while the MCP server runs, until stopped or expired.

Use the returned `id` in subsequent calls to the same MCP server. Replace `TUNNEL_ID` below:

```json
{"name":"kubectl_port_forward_status","arguments":{"id":"TUNNEL_ID"}}
```

```json
{"name":"kubectl_port_forward_stop","arguments":{"id":"TUNNEL_ID"}}
```

List tracked tunnels:

```json
{"name":"kubectl_port_forward_list","arguments":{}}
```

Status and stop calls return the tunnel result. List returns `{"tunnels":[...]}`. Do not send another `context` with an ID: the tunnel already identifies its context. A new CLI invocation cannot manage an MCP tunnel.

The result marks `read_only` and `traffic_sanitized` as `false`. Start and stop tools are not marked read-only. Tunnel IDs belong to the running MCP server. Normal server shutdown closes its tunnels. Finished tunnel records are removed when another tunnel starts.

Tunnels share the two-process limit with reads. Two active tunnels can temporarily prevent additional reads. Stop a tunnel to free a process slot.

## Status and failures

| Result | Meaning or action |
| --- | --- |
| `active` | Tunnel ready at the returned local address and port. |
| `stopped` | Explicit stop or CLI cancellation. |
| `expired` | Configured lifetime elapsed. |
| `closed` | kubectl exited before expiry without an explicit stop. Start a new tunnel if needed. |
| Port forwarding disabled | Enable `allow_port_forward` for the selected context. Restart MCP, or rebuild a built-in default. |
| Startup failed | Check target existence, Kubernetes permissions, and local port availability. |
| Toolbox busy | Stop an active tunnel or wait for a read to finish. |
| Unknown tunnel ID | Use an ID from the same running MCP server. Finished records may have been removed. |

The CLI exits with code 0 after a stop or expiry, and code 1 after an unexpected closure. Normal shutdown closes tunnels. Forced termination may prevent cleanup. Use process supervision when cleanup after a crash is required.

## Verification

Tests cover path and query bypass attempts, namespace restrictions, disabled forwarding, port validation, echo traffic through a subprocess fixture, expiration, cancellation, occupied ports, concurrency, and MCP shutdown. Real kubectl tests cover raw GET requests and tunnel startup/expiration against a local TLS mock API. The forwarding mock tests protocol upgrade and listener lifecycle, not remote pod traffic. No production cluster was accessed.

Run the integration tests with an installed kubectl:

```sh
TOOLBOX_TEST_KUBECTL=/absolute/path/kubectl go test -race -p=1 ./...
```
