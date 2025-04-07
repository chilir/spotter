# /apps/spotter/Dockerfile

FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim AS builder

ARG MODEL_NAME="PekingU/rtdetr_v2_r101vd"
ENV MODEL_NAME=${MODEL_NAME} \
    HF_HOME=/opt/hf_cache \
    PYTHONFAULTHANDLER=1 \
    PYTHONUNBUFFERED=1 \
    UV_COMPILE_BYTECODE=1

WORKDIR /app

COPY pyproject.toml uv.lock .python-version README.md ./
COPY src ./src
RUN uv sync --frozen --no-cache

RUN mkdir -p ${HF_HOME}
RUN uv run spotter_download


FROM python:3.12-slim-bookworm AS runner

ARG MODEL_NAME="PekingU/rtdetr_v2_r101vd"
ENV MODEL_NAME=${MODEL_NAME} \
    HF_HOME=/opt/hf_cache \
    PYTHONFAULTHANDLER=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app
COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /app/src /app/src
COPY --from=builder ${HF_HOME} ${HF_HOME}

ENV PATH="/app/.venv/bin:$PATH"
