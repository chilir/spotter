# /apps/spotter/Dockerfile

FROM rayproject/ray:2.44.1-py312-cpu-aarch64 AS runner

ARG MODEL_NAME="PekingU/rtdetr_v2_r101vd"
ENV MODEL_NAME=${MODEL_NAME} \
    HF_HUB_DISABLE_PROGRESS_BARS=1 \
    PYTHONFAULTHANDLER=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

COPY apps/spotter/src ./src
COPY apps/spotter/pyproject.toml ./

RUN pip install --no-cache-dir .
RUN $(dirname $(which pip))/spotter_download
