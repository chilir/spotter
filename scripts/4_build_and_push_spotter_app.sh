#!/bin/bash

set -euo pipefail

VM_IP=$(multipass list --format=csv | grep microk8s-vm | cut -d',' -f3)

LOCAL_TAG="spotter:test_debug"
REGISTRY_TAG="${VM_IP}:32000/${LOCAL_TAG}" # microk8s registry target

DOCKERFILE_PATH="apps/spotter/Dockerfile"

if [ ! -f "${DOCKERFILE_PATH}" ]; then
    echo "Error: Dockerfile not found at ${DOCKERFILE_PATH}"
    exit 1
fi

echo "--- Building Docker image for spotter app ---"
echo "Dockerfile: ${DOCKERFILE_PATH}"

docker build --no-cache -t "${LOCAL_TAG}" -f "${DOCKERFILE_PATH}" .

echo "--- Tagging image for microk8s registry (${REGISTRY_TAG}) ---"
docker tag "${LOCAL_TAG}" "${REGISTRY_TAG}"

echo "--- Pushing image to microk8s registry (${REGISTRY_TAG}) ---"
# Ensure docker is configured for insecure microk8s registry VM_IP:32000
docker push "${REGISTRY_TAG}"

echo "--- Build and push complete! ---"
echo "Image available as: ${REGISTRY_TAG}"
