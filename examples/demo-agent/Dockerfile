# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89

FROM python:3.12.11-slim@sha256:47ae396f09c1303b8653019811a8498470603d7ffefc29cb07c88f1f8cb3d19f AS build

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    UV_COMPILE_BYTECODE=1

RUN DEBIAN_FRONTEND=noninteractive apt-get update \
    && apt-get install --yes --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*
RUN python -m pip install --no-cache-dir uv==0.12.0

WORKDIR /workspace
COPY sdk/python/ sdk/python/
COPY examples/demo-agent/ examples/demo-agent/
RUN uv sync --project examples/demo-agent --frozen --no-dev --no-editable

FROM python:3.12.11-slim@sha256:47ae396f09c1303b8653019811a8498470603d7ffefc29cb07c88f1f8cb3d19f

ENV PATH="/workspace/examples/demo-agent/.venv/bin:${PATH}" \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

COPY --from=build /workspace/examples/demo-agent/.venv/ /workspace/examples/demo-agent/.venv/

USER 65532:65532
EXPOSE 39100 39200
CMD ["agentshark-demo-runner"]
