import os
from typing import Any

from fastapi import FastAPI

app = FastAPI(
    title="ARMAN AI Service",
    version="0.1.0",
    description="Provider-agnostic AI, OCR, document, and audio service boundary.",
)


@app.get("/health")
def health() -> dict[str, Any]:
    return {"success": True, "data": {"service": "arman-ai", "status": "healthy"}}


@app.get("/ready")
def ready() -> dict[str, Any]:
    provider = os.getenv("AI_PROVIDER", "")
    return {
        "success": True,
        "data": {
            "status": "ready" if provider else "configuration_required",
            "providerConfigured": bool(provider),
        },
    }


@app.post("/v1/generate")
def generate(payload: dict[str, Any]) -> dict[str, Any]:
    return {
        "success": False,
        "data": None,
        "message": "AI provider configuration is required before generation is enabled.",
        "error": {"code": "AI_PROVIDER_NOT_CONFIGURED"},
    }
