#!/bin/bash

set -euo pipefail

# assumes microk8s is already installed and started
microk8s enable dns registry
microk8s helm repo add kuberay https://ray-project.github.io/kuberay-helm/
microk8s helm repo update
microk8s helm install kuberay-operator kuberay/kuberay-operator --version 1.3.1 --namespace spotter --create-namespace
