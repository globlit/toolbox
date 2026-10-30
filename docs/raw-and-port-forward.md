# Raw GET and port forwarding

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

Exec, attach, proxy, Secrets, ConfigMaps, watch, unknown endpoints, encoded path segments, and duplicate query keys are rejected. Use `kubectl_logs` or `toolbox kubectl logs` for bounded log reads. Raw requests cannot be combined with resource names or other CLI flags. Returned JSON receives the same sanitization as typed reads. [`kubectl get` reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_get/).

## Enable tunnels

Add these fields to the approved configuration's `kubectl` object:

```json
{
  "allow_port_forward": true,
  "port_forward_max_seconds": 300
}
```

Rebuild with `scripts/provision`. Forwarding is disabled by default. The lifetime limit accepts 1 to 3,600 seconds.

Grant the existing identity `get` and `create` on `pods/portforward` in each approved namespace. Review the [optional role and binding](../deploy/kubernetes/port-forward.optional.yaml). Existing pod, service, and deployment reads remain necessary for target resolution. The reader role is unchanged.

Tunnel traffic can perform writes and disclose secrets. Toolbox does not sanitize it. Kubernetes authorizes tunnel establishment, not individual requests through that tunnel.

## CLI

Replace `api-0`, `api`, and `apps` with your pod, service, and namespace names:

```sh
toolbox kubectl port-forward api-0 18080:8080 -n apps --duration 5m
toolbox kubectl port-forward service/api :80 -n apps --duration 1m
toolbox kubectl port-forward deployment/api 18080:8080 19090:9090 -n apps
```

The first result reports `status: "active"`, the tunnel `id`, and assigned ports. The command stays running. Press Ctrl+C to stop it. A final result reports `stopped`, `expired`, or `closed`. Startup errors return a generic diagnostic without raw subprocess output.

Targets support pod names or `pod/`, `service/`, and `deployment/` prefixes. Ports use numeric `[LOCAL:]REMOTE` mappings. Supply one to eight mappings. `:REMOTE` or `0:REMOTE` requests a random local port. Fixed local ports must be 1024–65535. Remote ports must be 1–65535. Named ports are unsupported.

Every tunnel binds to `127.0.0.1`. Arbitrary bind addresses are rejected. The default lifetime is the operator limit. This interface supports a restricted subset of [`kubectl port-forward`](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_port-forward/).

## MCP

Start a tunnel:

```json
{"name":"kubectl_port_forward","arguments":{"target":"service/api","ports":[":80"],"namespace":"apps","duration_seconds":60}}
```

Use the returned `id` in subsequent calls. Replace `TUNNEL_ID` below:

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

The result marks `read_only` and `traffic_sanitized` as `false`. Start and stop tools are not marked read-only. Tunnel IDs belong to the running MCP server. Normal server shutdown closes its tunnels. Finished tunnel records are removed when another tunnel starts.

Tunnels share the two-process limit with reads. Two active tunnels can temporarily prevent additional reads. Stop a tunnel to free a process slot.

## Verification

Tests cover path and query bypass attempts, namespace restrictions, disabled forwarding, port validation, echo traffic through a subprocess fixture, expiration, cancellation, occupied ports, concurrency, and MCP shutdown. Real kubectl tests cover raw GET requests and tunnel startup/expiration against a local TLS mock API. The forwarding mock tests protocol upgrade and listener lifecycle, not remote pod traffic. No production cluster was accessed.

Run the integration tests with an installed kubectl:

```sh
TOOLBOX_TEST_KUBECTL=/absolute/path/kubectl go test -race -p=1 ./...
```
