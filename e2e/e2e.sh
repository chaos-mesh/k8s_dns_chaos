#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
NAMESPACE="chaos-dns-e2e"
IMAGE="ghcr.io/chaos-mesh/chaos-coredns:e2e-test"
MINIKUBE_PROFILE="chaos-dns-e2e"

cleanup() {
    echo "==> Cleaning up..."
    pkill -f "port-forward.*${NAMESPACE}" || true
    minikube delete -p ${MINIKUBE_PROFILE} || true
}
trap cleanup EXIT

echo "==> Starting minikube (if not running)..."
minikube status -p ${MINIKUBE_PROFILE} > /dev/null 2>&1 || minikube start -p ${MINIKUBE_PROFILE} --driver=docker

echo "==> Building image..."
cd "${PROJECT_DIR}"
DOCKER_BUILDKIT=1 docker build -t ${IMAGE} .

echo "==> Loading image into minikube..."
minikube image load -p ${MINIKUBE_PROFILE} ${IMAGE}

echo "==> Deploying manifests..."
kubectl apply -f "${SCRIPT_DIR}/manifests/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-rbac.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-configmap.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/chaos-coredns-service.yaml"

echo "==> Waiting for chaos-coredns to be ready..."
kubectl -n ${NAMESPACE} wait --for=condition=ready pod -l app=chaos-coredns --timeout=120s

echo "==> Getting chaos-coredns service IP and deploying test pod..."
CHAOS_IP=$(kubectl -n ${NAMESPACE} get svc chaos-coredns -o jsonpath='{.spec.clusterIP}')
echo "    Chaos CoreDNS IP: ${CHAOS_IP}"
sed "s/CHAOS_COREDNS_IP/${CHAOS_IP}/g" "${SCRIPT_DIR}/manifests/test-pod.yaml" | kubectl apply -f -
kubectl -n ${NAMESPACE} wait --for=condition=ready pod test-client --timeout=60s

echo "==> Starting port-forward..."
kubectl -n ${NAMESPACE} port-forward svc/chaos-coredns 9288:9288 &
PORT_FORWARD_PID=$!
sleep 3

echo "==> Running E2E tests..."
cd "${PROJECT_DIR}"
go test -v -timeout 10m ./e2e/...

echo "==> E2E tests passed!"
