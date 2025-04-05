#!/bin/bash

set -euo pipefail

VM_IP=$(multipass list --format=csv | grep microk8s-vm | cut -d',' -f3)

LOCAL_TAG="spotter-manager:test_debug"
REGISTRY_TAG="${VM_IP}:32000/${LOCAL_TAG}" # microk8s registry target

APP_BUILD_CONTEXT="apps/spotter-manager"
DOCKERFILE_PATH="${APP_BUILD_CONTEXT}/Dockerfile"

if [ ! -f "${DOCKERFILE_PATH}" ]; then
    echo "Error: Dockerfile not found at ${DOCKERFILE_PATH}"
    exit 1
fi

echo "--- Building Docker image for spotter-manager ---"
echo "Dockerfile: ${DOCKERFILE_PATH}"
echo "Build Context: ${APP_BUILD_CONTEXT}"

docker build -t "${LOCAL_TAG}" -f "${DOCKERFILE_PATH}" "${APP_BUILD_CONTEXT}"

echo "--- Tagging image for microk8s registry (${REGISTRY_TAG}) ---"
docker tag "${LOCAL_TAG}" "${REGISTRY_TAG}"

echo "--- Pushing image to microk8s registry (${REGISTRY_TAG}) ---"
# Ensure docker is configured for insecure microk8s registry VM_IP:32000
docker push "${REGISTRY_TAG}"

echo "--- Build and push complete! ---"
echo "Image available as: ${REGISTRY_TAG}"
