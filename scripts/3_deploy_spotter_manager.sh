#!/bin/bash

set -euo pipefail

MANIFEST_FILE="configs/spotter-manager-deployment.yaml"
NAMESPACE="spotter"
DEPLOYMENT_NAME="kuberay-manager-deployment"
SERVICE_NAME="kuberay-manager-service"

echo "--- Applying Kubernetes manifest: ${MANIFEST_FILE} ---"
microk8s kubectl apply -f "${MANIFEST_FILE}"

echo "--- Waiting for deployment rollout: ${DEPLOYMENT_NAME} in namespace ${NAMESPACE} ---"
microk8s kubectl rollout status deployment/"${DEPLOYMENT_NAME}" -n "${NAMESPACE}" --timeout=120s

echo "--- Deployment successful! ---"

echo "--- Getting Service Info (${SERVICE_NAME}) ---"
microk8s kubectl get svc "${SERVICE_NAME}" -n "${NAMESPACE}"


NODEPORT=$(microk8s kubectl get svc "${SERVICE_NAME}" -n "${NAMESPACE}" -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || echo "N/A")

if [[ "$NODEPORT" != "N/A" && -n "$NODEPORT" ]]; then
    echo "Service NodePort: ${NODEPORT}"
    VM_IP=$(multipass list --format=csv | grep microk8s-vm | cut -d',' -f3)
    if [[ -n "$VM_IP" ]]; then
        echo "Access via: http://${VM_IP}:${NODEPORT}"
    else
        echo "Could not determine MicroK8s VM IP."
    fi
fi
