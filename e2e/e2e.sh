#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
NAMESPACE="chaos-dns-e2e"
IMAGE="ghcr.io/chaos-mesh/chaos-coredns:e2e-test"

for tool in docker minikube kubectl helm go; do
    command -v "$tool" > /dev/null || { echo "Missing required command: $tool" >&2; exit 1; }
done

RUN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/chaos-dns-e2e.XXXXXXXX")
MINIKUBE_PROFILE="chaos-dns-e2e-$(basename "$RUN_DIR" | tr '[:upper:]' '[:lower:]' | cut -d. -f2)"
export KUBECONFIG="${RUN_DIR}/kubeconfig"
PORT_FORWARD_PID=""
OWNS_PROFILE=false
ARTIFACT_DIR="${E2E_ARTIFACT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/chaos-dns-e2e-artifacts.XXXXXXXX")}"

kube() {
    kubectl --request-timeout=15s "$@"
}

collect_diagnostics() {
    kube get pods -A -o wide > "${ARTIFACT_DIR}/pods.txt" 2>&1 || true
    kube get events -A --sort-by=.metadata.creationTimestamp > "${ARTIFACT_DIR}/events.txt" 2>&1 || true
    kube -n "$NAMESPACE" get dnschaos -o yaml > "${ARTIFACT_DIR}/dnschaos.yaml" 2>&1 || true
    kube -n "$NAMESPACE" describe pods > "${ARTIFACT_DIR}/pod-descriptions.txt" 2>&1 || true
    for component in chaos-coredns controller-manager chaos-daemon; do
        selector="app.kubernetes.io/component=${component}"
        if [[ "$component" == chaos-coredns ]]; then
            selector="app=chaos-coredns"
        fi
        kube -n "$NAMESPACE" logs -l "$selector" --all-containers --prefix --tail=-1 > "${ARTIFACT_DIR}/${component}.log" 2>&1 || true
        kube -n "$NAMESPACE" logs -l "$selector" --all-containers --prefix --tail=-1 --previous > "${ARTIFACT_DIR}/${component}-previous.log" 2>&1 || true
    done
}

cleanup() {
    result=$?
    trap - EXIT
    if [[ "$result" -ne 0 && "$OWNS_PROFILE" == true ]]; then
        echo "==> Collecting diagnostics in ${ARTIFACT_DIR}"
        collect_diagnostics
    fi
    if [[ -n "$PORT_FORWARD_PID" ]]; then
        kill "$PORT_FORWARD_PID" 2>/dev/null || true
        wait "$PORT_FORWARD_PID" 2>/dev/null || true
    fi
    if [[ "$OWNS_PROFILE" == true ]]; then
        echo "==> Deleting profile ${MINIKUBE_PROFILE}"
        if ! minikube delete -p "$MINIKUBE_PROFILE" > "${ARTIFACT_DIR}/minikube-delete.log" 2>&1; then
            echo "Failed to delete ${MINIKUBE_PROFILE}; see ${ARTIFACT_DIR}/minikube-delete.log" >&2
            result=1
        fi
    fi
    rm -rf "$RUN_DIR"
    echo "==> E2E artifacts: ${ARTIFACT_DIR}"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$ARTIFACT_DIR"
ARTIFACT_DIR="$(cd "$ARTIFACT_DIR" && pwd)"
printf '%s\n' "$MINIKUBE_PROFILE" > "${ARTIFACT_DIR}/minikube-profile.txt"
echo "==> Starting private minikube profile ${MINIKUBE_PROFILE}"
OWNS_PROFILE=true
minikube start -p "$MINIKUBE_PROFILE" --driver=docker --container-runtime=containerd --wait-timeout=6m 2>&1 | tee "${ARTIFACT_DIR}/minikube-start.log"

echo "==> Building image..."
cd "$PROJECT_DIR"
DOCKER_BUILDKIT=1 docker build -t "$IMAGE" . 2>&1 | tee "${ARTIFACT_DIR}/image-build.log"
minikube image load -p "$MINIKUBE_PROFILE" "$IMAGE"

echo "==> Deploying branch DNS server..."
kube apply -f "${SCRIPT_DIR}/manifests/namespace.yaml"
kube apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-rbac.yaml"
kube apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-configmap.yaml"
kube apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-deployment.yaml"
kube apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-service.yaml"
kube --request-timeout=190s -n "$NAMESPACE" rollout status deployment/chaos-coredns --timeout=180s

echo "==> Installing Chaos Mesh controller and daemon..."
helm install chaos-mesh chaos-mesh --repo https://charts.chaos-mesh.org --version 2.8.5 \
    --namespace "$NAMESPACE" --wait --timeout 5m \
    --set dnsServer.create=false --set dnsServer.name=chaos-coredns \
    --set dashboard.create=false --set controllerManager.replicaCount=1 \
    --set controllerManager.leaderElection.enabled=false \
    --set chaosDaemon.runtime=containerd --set chaosDaemon.socketPath=/run/containerd/containerd.sock \
    2>&1 | tee "${ARTIFACT_DIR}/helm-install.log"
kube --request-timeout=190s -n "$NAMESPACE" rollout status deployment/chaos-controller-manager --timeout=180s
kube --request-timeout=190s -n "$NAMESPACE" rollout status daemonset/chaos-daemon --timeout=180s

echo "==> Deploying DNS clients and probe Services..."
CHAOS_IP=$(kube -n "$NAMESPACE" get svc chaos-coredns -o jsonpath='{.spec.clusterIP}')
sed "s/CHAOS_COREDNS_IP/${CHAOS_IP}/g" "${SCRIPT_DIR}/manifests/test-pod.yaml" | kube apply -f -
kube apply -f "${SCRIPT_DIR}/manifests/controller-fixtures.yaml"
kube --request-timeout=130s -n "$NAMESPACE" wait --for=condition=ready pod test-client --timeout=120s
kube --request-timeout=130s -n "$NAMESPACE" wait --for=condition=ready pod -l e2e=dns-controller --timeout=120s

echo "==> Starting gRPC port-forward..."
kubectl -n "$NAMESPACE" port-forward --address=127.0.0.1 svc/chaos-coredns :9288 > "${ARTIFACT_DIR}/port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!
GRPC_PORT=""
for ((attempt = 0; attempt < 30; attempt++)); do
    if ! kill -0 "$PORT_FORWARD_PID" 2>/dev/null; then
        cat "${ARTIFACT_DIR}/port-forward.log" >&2
        exit 1
    fi
    GRPC_PORT=$(sed -n 's/^Forwarding from 127\.0\.0\.1:\([0-9]*\) -> 9288$/\1/p' "${ARTIFACT_DIR}/port-forward.log")
    if [[ -n "$GRPC_PORT" ]]; then
        break
    fi
    sleep 1
done
if [[ -z "$GRPC_PORT" ]]; then
    echo "Port-forward did not become ready" >&2
    exit 1
fi
export E2E_GRPC_ADDRESS="127.0.0.1:${GRPC_PORT}"

echo "==> Running E2E tests..."
go test -v -count=1 -timeout 15m ./e2e/... 2>&1 | tee "${ARTIFACT_DIR}/go-test.log"
echo "==> E2E tests passed!"
