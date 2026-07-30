"""Private HTTP protocol between the AgentsharkX BFF and Demo Runner."""

from __future__ import annotations

import secrets
from typing import Annotated
from uuid import UUID

from fastapi import Depends, FastAPI, Header, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from agentshark_demo.config import RunnerSettings
from agentshark_demo.models import RunnerSnapshot, RunnerStartRequest
from agentshark_demo.runner.registry import RegistryError, RunRegistry


def create_app(settings: RunnerSettings, registry: RunRegistry) -> FastAPI:
    app = FastAPI(
        title="Agentshark Demo Runner",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
    )

    def authorize(
        authorization: Annotated[str | None, Header()] = None,
    ) -> None:
        supplied = authorization or ""
        expected = f"Bearer {settings.token}"
        if not secrets.compare_digest(supplied.encode(), expected.encode()):
            raise RunnerUnauthorized(RunnerUnauthorized.code)

    internal = Depends(authorize)

    @app.exception_handler(RegistryError)
    async def registry_error_handler(
        request: Request, exc: RegistryError
    ) -> JSONResponse:
        _ = request
        return JSONResponse(status_code=exc.status_code, content={"code": exc.code})

    @app.exception_handler(RequestValidationError)
    async def validation_error_handler(
        request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        _ = (request, exc)
        return JSONResponse(status_code=422, content={"code": "demo_run_invalid"})

    @app.get("/healthz")
    def health() -> dict[str, object]:
        active_run_id = registry.active_run_id()
        return {
            "status": "healthy",
            "service": "agentshark-demo-runner",
            "maxConcurrency": settings.max_concurrency,
            "activeRunId": str(active_run_id) if active_run_id is not None else None,
        }

    @app.post(
        "/internal/v1/runs",
        response_model=RunnerSnapshot,
        response_model_by_alias=True,
        status_code=202,
        dependencies=[internal],
    )
    def start_run(request: RunnerStartRequest) -> RunnerSnapshot:
        snapshot, _created = registry.start(request)
        return snapshot

    @app.get(
        "/internal/v1/runs/{run_id}",
        response_model=RunnerSnapshot,
        response_model_by_alias=True,
        dependencies=[internal],
    )
    def get_run(run_id: UUID) -> RunnerSnapshot:
        return registry.get(run_id)

    @app.post(
        "/internal/v1/runs/{run_id}/cancel",
        response_model=RunnerSnapshot,
        response_model_by_alias=True,
        dependencies=[internal],
    )
    def cancel_run(run_id: UUID) -> RunnerSnapshot:
        return registry.cancel(run_id)

    return app


class RunnerUnauthorized(RegistryError):
    code = "demo_runner_unauthorized"
    status_code = 401
