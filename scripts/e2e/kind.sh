#!/usr/bin/env bash
set -euo pipefail
umask 077
cd "$(dirname "$0")/../.."
for binary in kind docker kubectl go python3; do command -v "$binary" >/dev/null; done
scratch=$(mktemp -d)
mkdir "$scratch/image"
cluster="toolbox-e2e-$(date +%s)-$$"
fixture_image="toolbox-e2e-fixture:$cluster"
cleanup() {
  kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
  docker image rm "$fixture_image" >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
node_image=${TOOLBOX_KIND_NODE_IMAGE:-kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d}
kind create cluster --name "$cluster" --image "$node_image" --kubeconfig "$scratch/admin.kubeconfig" --wait 120s
admin=(kubectl --kubeconfig "$scratch/admin.kubeconfig" --context "kind-$cluster")
for namespace in apps private other; do
  "${admin[@]}" create namespace "$namespace"
  "${admin[@]}" -n "$namespace" create configmap "settings-$namespace" --from-literal=ordinary=SYNTHETIC_CM_SECRET
  "${admin[@]}" -n "$namespace" create secret generic hidden --from-literal=password=SYNTHETIC_SECRET_ONLY
done
"${admin[@]}" -n apps create serviceaccount toolbox-reader
"${admin[@]}" -n apps create serviceaccount toolbox-limited
"${admin[@]}" create clusterrole toolbox-e2e-reader --verb=get,list --resource=pods,configmaps,namespaces,pods/log
"${admin[@]}" create clusterrolebinding toolbox-e2e-reader --clusterrole=toolbox-e2e-reader --serviceaccount=apps:toolbox-reader
"${admin[@]}" -n apps create rolebinding toolbox-e2e-limited --clusterrole=toolbox-e2e-reader --serviceaccount=apps:toolbox-limited
"${admin[@]}" -n apps create token toolbox-reader --duration=30m > "$scratch/reader.token"
"${admin[@]}" -n apps create token toolbox-limited --duration=30m > "$scratch/limited.token"
"${admin[@]}" config view --raw --flatten --minify -o json > "$scratch/admin.json"
export TOOLBOX_E2E_SCRATCH="$scratch"
export TOOLBOX_E2E_KUBECTL
TOOLBOX_E2E_KUBECTL=$(command -v kubectl)
python3 - <<'PY'
import json, os, pathlib
root=pathlib.Path(os.environ['TOOLBOX_E2E_SCRATCH'])
admin=json.loads((root/'admin.json').read_text())
kube={'apiVersion':'v1','kind':'Config','clusters':admin['clusters'],'users':[], 'contexts':[], 'current-context':'reader'}
for name in ['reader','limited']:
    kube['users'].append({'name':name,'user':{'token':(root/(name+'.token')).read_text().strip()}})
    kube['contexts'].append({'name':name,'context':{'cluster':kube['clusters'][0]['name'],'user':name}})
(root/'reader.kubeconfig').write_text(json.dumps(kube))
config={'kubectl':{'binary':os.environ['TOOLBOX_E2E_KUBECTL'],'kubeconfig':str(root/'reader.kubeconfig'),'contexts':[
 {'name':'reader','default_namespace':'apps','namespaces':['*']},
 {'name':'limited','default_namespace':'apps','namespaces':['*']} ]}}
(root/'toolbox.json').write_text(json.dumps(config))
fixtures=json.loads(pathlib.Path('internal/sanitize/testdata/complex_logs.json').read_text())
lines=[]
for i, fixture in enumerate(fixtures):
    if fixture['closed']:
        lines += [f'INFO checkpoint-{i:03d} ready', fixture['input']]
lines += ['INFO healthy-after']
(root/'image'/'logs.txt').write_text('\n'.join(lines)+'\n')
PY
CGO_ENABLED=0 GOOS=linux go build -o "$scratch/image/log-emitter" ./scripts/e2e/log-emitter
cat > "$scratch/image/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY log-emitter /log-emitter
COPY logs.txt /logs.txt
USER 65532:65532
ENTRYPOINT ["/log-emitter"]
DOCKERFILE
# The scratch image needs read access for its non-root process.
chmod 644 "$scratch/image/logs.txt"
chmod 755 "$scratch/image/log-emitter"
docker build --tag "$fixture_image" "$scratch/image"
kind load docker-image --name "$cluster" "$fixture_image"
"${admin[@]}" -n apps run log-fixture --image="$fixture_image" --image-pull-policy=Never --restart=Never
"${admin[@]}" -n apps wait --for=condition=Ready pod/log-fixture --timeout=120s
go build -o "$scratch/toolbox" .
TOOLBOX_KIND_CONFIG="$scratch/toolbox.json" TOOLBOX_KIND_BINARY="$scratch/toolbox" \
  TOOLBOX_TEST_KUBECTL="$TOOLBOX_E2E_KUBECTL" GOMAXPROCS=2 \
  go test -race -p=1 ./internal/integrations/kubectl -run '^(TestKindEndToEnd|TestSecurityRegressionsOverCLIAndMCP)$' -count=1 -timeout=5m
