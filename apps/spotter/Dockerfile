# /apps/spotter/Dockerfile

FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim AS builder

ARG MODEL_NAME="PekingU/rtdetr_v2_r101vd"
ENV MODEL_NAME=${MODEL_NAME} \
    HF_HOME=/opt/hf_cache \
    PYTHONFAULTHANDLER=1 \
    PYTHONUNBUFFERED=1 \
    UV_COMPILE_BYTECODE=1

WORKDIR /app

COPY apps/spotter/pyproject.toml apps/spotter/uv.lock apps/spotter/.python-version apps/spotter/README.md ./
COPY apps/spotter/src ./src
RUN uv sync --frozen --no-cache

RUN mkdir -p ${HF_HOME}
RUN uv run spotter_download


FROM rayproject/ray:2.44.1-py312-cpu-aarch64 AS runner

ARG MODEL_NAME="PekingU/rtdetr_v2_r101vd"
ENV MODEL_NAME=${MODEL_NAME} \
    HF_HOME=/opt/hf_cache \
    PYTHONFAULTHANDLER=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

# Copy application code and dependency definition
COPY apps/spotter/src ./src
COPY apps/spotter/pyproject.toml apps/spotter/.python-version apps/spotter/README.md ./

# Copy downloaded models from builder stage
COPY --from=builder ${HF_HOME} ${HF_HOME}

# Install application dependencies (Ray is already in the base image)
# pip should be smart enough to handle the existing Ray installation
RUN pip install --no-cache-dir .

# No need to set PATH, ENTRYPOINT, or CMD - rely on the base Ray image defaults
# and KubeRay injection.
