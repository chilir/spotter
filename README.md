# spotter

spotter serves as a reference project designed for experimenting with Kubernetes and KubeRay
locally. As an example use case, spotter demonstrates how to dynamically deploy, manage, and
interact with an object detection model running as a RayService.

The project consists of two main components:

* `spotter-manager`: A Go-based Kubernetes management service that provides an API and a simple web
UI to manage the ML service deployment on Kubernetes.
* `spotter`: A Python-based ML service using Ray Serve for detecting amenities in images.

`spotter-manager` handles deploying the `spotter` application as a KubeRay RayService on
Kubernetes and proxies detection requests to the active service.

The deployment process involves `spotter-manager` generating a `RayService` manifest by populating a
template ([`configs/rayservice-template.yaml`](configs/rayservice-template.yaml)). Once
`spotter-manager` applies this manifest, KubeRay uses it to manage the underlying Ray cluster,
automatically handling autoscaling within the resource limits defined in the manifest.

## Features

* Manages deployment and deletion of  `spotter` as a KubeRay RayService via HTTP API and web UI
* Utilizes configurable object detection models from HuggingFace
* Provides endpoints for service management (`/deploy`, `/delete`) and amenity detection (`/detect`)
* Includes a simple web frontend for deploying/deleting the service and submitting image URLs for
  inference

## Prerequisites

* Docker
* A local Kubernetes cluster (e.g. MicroK8s)

## Quickstart

The setup scripts under `scripts/` assumes MicroK8s is set up and a VM is live on an Apple Silicon
Mac. Some modifications will be necessary if using a different Kubernetes distribution or a
different operating system.

```bash
# clone source code
git clone https://github.com/chilir/spotter.git
cd spotter

# set up microk8s add-ons and install the KubeRay operator
./scripts/1_microk8s_setup.sh

# build and push spotter-manager Docker image to local microk8s registry
./scripts/2_build_and_push_spotter_manager.sh

# deploy spotter-manager
./scripts/3_deploy_spotter_manager.sh

# build and push spotter
./scripts/4_build_and_push_spotter_app.sh

```

## Usage

### Web UI

Access the `spotter-manager` service (e.g., `http://<vm_ip>:8080`). The UI allows you to:

* Deploy the `spotter` service by entering the Docker image name
* Submit image URLs for amenity detection
* Delete the `spotter` service

## Configuration

* `spotter-manager`:
  * Uses [`configs/rayservice-template.yaml`](configs/rayservice-template.yaml) to generate the
    RayService manifest for `spotter`
  * Deployed using [`configs/spotter-manager-deployment.yaml`](configs/spotter-manager-deployment.yaml)
* `spotter`:
  * `MODEL_NAME` environment variable (or Docker build argument) specifies the HuggingFace object
    detection model. Default:
    [`PekingU/rtdetr_v2_r101vd`](https://huggingface.co/PekingU/rtdetr_v2_r101vd).

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
