# -*- coding: utf-8 -*-
"""AgentScope Python MCP gateway compatibility asset.

The host uploads this standalone script into the remote sandbox. Running it
directly avoids importing the full optional ``agentscope.workspace`` dependency
tree through ``python -m``. HTTP listens on the sandbox loopback only.
"""

import argparse
import asyncio
import json
from typing import Any

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import PlainTextResponse

from agentscope.mcp import MCPClient


class _State:
    """Hold MCP runtime state shared by the FastAPI routes."""

    def __init__(self) -> None:
        self.clients: dict[str, MCPClient] = {}
        self.lock = asyncio.Lock()


async def _build_client(spec: dict[str, Any]) -> MCPClient:
    """Validate an MCP config, connect stateful clients, and prime tools."""
    client = MCPClient.model_validate(spec)
    if client.is_stateful:
        await client.connect()
    await client.list_raw_tools()
    return client


def _build_app(state: _State) -> FastAPI:
    """Build the gateway application used only through sandbox loopback."""
    app = FastAPI(title="agentscope-workspace-mcp-gateway")

    @app.get("/health")
    async def _health() -> PlainTextResponse:
        return PlainTextResponse("ok")

    @app.get("/mcps")
    async def _list_mcps() -> list[dict[str, Any]]:
        return [c.model_dump(mode="json") for c in state.clients.values()]

    @app.post("/mcps")
    async def _add_mcp(request: Request) -> dict[str, Any]:
        body = await request.json()
        name = body.get("name", "")
        if not name:
            raise HTTPException(400, "name required")
        async with state.lock:
            if name in state.clients:
                raise HTTPException(409, f"{name!r} already exists")
            try:
                client = await _build_client(body)
            except HTTPException:
                raise
            except Exception as exc:  # noqa: BLE001
                raise HTTPException(500, f"connect failed: {exc}") from exc
            state.clients[name] = client
        return {"ok": True}

    @app.delete("/mcps/{name}")
    async def _remove_mcp(name: str) -> dict[str, Any]:
        async with state.lock:
            client = state.clients.pop(name, None)
            if client is None:
                raise HTTPException(404, f"{name!r} not found")
            if client.is_stateful and client.is_connected:
                await client.close()
        return {"ok": True}

    @app.get("/mcps/{name}/tools")
    async def _list_tools(name: str) -> list[dict[str, Any]]:
        client = state.clients.get(name)
        if client is None:
            raise HTTPException(404, f"{name!r} not found")
        raw = await client.list_raw_tools()
        return [tool.model_dump(mode="json") for tool in raw]

    @app.post("/mcps/{name}/tools/{tool}")
    async def _call_tool(
        name: str,
        tool: str,
        request: Request,
    ) -> dict[str, Any]:
        client = state.clients.get(name)
        if client is None:
            raise HTTPException(404, f"{name!r} not found")
        body = await request.json()
        arguments = body.get("arguments") or {}
        try:
            tool_obj = await client.get_tool(tool)
            chunk = await tool_obj(**arguments)
        except ValueError as exc:
            raise HTTPException(404, str(exc)) from exc
        except Exception as exc:  # noqa: BLE001
            raise HTTPException(500, str(exc)) from exc
        return {"chunk": chunk.model_dump(mode="json")}

    return app


async def _connect_initial(
    state: _State,
    server_cfgs: list[dict[str, Any]],
) -> None:
    """Connect all MCP servers declared in the configuration file."""
    for cfg in server_cfgs:
        client = await _build_client(cfg)
        if client.name in state.clients:
            if client.is_stateful and client.is_connected:
                await client.close()
            raise ValueError(
                f"Duplicated server name in config: {client.name!r}",
            )
        state.clients[client.name] = client
        print(f"[gateway] connected {client.name!r}", flush=True)


async def _run(config_path: str, port: int) -> None:
    """Read configuration, connect MCP servers, and start uvicorn."""
    with open(config_path, encoding="utf-8") as config_file:
        servers = json.load(config_file)
    if not isinstance(servers, list):
        raise ValueError(
            "config file must be a JSON list of MCPClient specs, "
            f"got {type(servers).__name__}",
        )

    state = _State()
    await _connect_initial(state, servers)
    app = _build_app(state)
    print(
        f"[gateway] serving {len(state.clients)} MCPs on :{port}",
        flush=True,
    )

    import uvicorn

    uvicorn_config = uvicorn.Config(
        app,
        host="127.0.0.1",
        port=port,
        log_level="info",
    )
    server = uvicorn.Server(uvicorn_config)
    try:
        await server.serve()
    finally:
        for client in list(state.clients.values()):
            if client.is_stateful and client.is_connected:
                await client.close()


def main() -> None:
    """Run the gateway CLI."""
    parser = argparse.ArgumentParser(
        description="In-workspace MCP gateway (FastAPI)",
    )
    parser.add_argument("--config", required=True)
    parser.add_argument("--port", type=int, default=5600)
    args = parser.parse_args()
    asyncio.run(_run(args.config, args.port))


if __name__ == "__main__":
    main()
