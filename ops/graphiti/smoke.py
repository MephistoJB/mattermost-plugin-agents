#!/usr/bin/env python3
"""Exercise a local Graphiti MCP server with a disposable episode."""

import argparse
import json
import time
import urllib.error
import urllib.request
import uuid


class MCPClient:
    def __init__(self, url):
        self.url = url
        self.session_id = None
        self.request_id = 0

    def call(self, method, params):
        self.request_id += 1
        headers = {
            "Accept": "application/json, text/event-stream",
            "Content-Type": "application/json",
        }
        if self.session_id:
            headers["Mcp-Session-Id"] = self.session_id
        body = json.dumps(
            {"jsonrpc": "2.0", "id": self.request_id, "method": method, "params": params}
        ).encode()
        request = urllib.request.Request(self.url, body, headers, method="POST")
        with urllib.request.urlopen(request, timeout=30) as response:
            if response.headers.get("Mcp-Session-Id"):
                self.session_id = response.headers["Mcp-Session-Id"]
            raw = response.read().decode()
            if response.headers.get_content_type() == "text/event-stream":
                raw = "\n".join(
                    line[5:].strip()
                    for line in raw.splitlines()
                    if line.startswith("data:")
                )
            message = json.loads(raw)
        if "error" in message:
            raise RuntimeError(f"MCP {method}: {message['error']}")
        result = message.get("result", {})
        if result.get("isError"):
            raise RuntimeError(f"Graphiti {method}: {result.get('content')}")
        return result

    def tool(self, name, arguments):
        result = self.call("tools/call", {"name": name, "arguments": arguments})
        blocks = result.get("content", [])
        if blocks and blocks[0].get("type") == "text":
            try:
                parsed = json.loads(blocks[0]["text"])
                if isinstance(parsed, dict) and parsed.get("error"):
                    raise RuntimeError(f"Graphiti {name}: {parsed['error']}")
                return parsed
            except json.JSONDecodeError:
                return {"text": blocks[0]["text"]}
        return result.get("structuredContent", result)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18000/mcp")
    parser.add_argument("--group", default="personal-global")
    parser.add_argument("--timeout", type=int, default=120)
    args = parser.parse_args()

    client = MCPClient(args.url)
    client.call(
        "initialize",
        {
            "protocolVersion": "2025-03-26",
            "capabilities": {},
            "clientInfo": {"name": "graphiti-smoke", "version": "1"},
        },
    )
    tools = client.call("tools/list", {})
    names = {tool["name"] for tool in tools.get("tools", [])}
    required = {"add_memory", "search_memory_facts", "get_episodes", "delete_episode"}
    if not required.issubset(names):
        raise RuntimeError(f"Graphiti tools missing: {sorted(required - names)}")

    marker = "GRAPHITI_SMOKE_" + uuid.uuid4().hex[:16]
    body = f"For the {marker} project, the agreed test color is cobalt blue."
    print("MCP connected; adding synthetic episode", flush=True)
    client.tool(
        "add_memory",
        {
            "name": marker,
            "episode_body": body,
            "group_id": args.group,
            "source": "text",
            "source_description": "Disposable integration smoke test",
        },
    )
    found = False
    episode_id = None
    last_result = None
    try:
        deadline = time.monotonic() + args.timeout
        while time.monotonic() < deadline:
            result = client.tool(
                "search_memory_facts",
                {"query": "cobalt blue test color", "group_ids": [args.group], "max_facts": 10},
            )
            last_result = result
            if result.get("facts") and marker in json.dumps(result):
                found = True
                break
            time.sleep(5)
        if not found:
            raise RuntimeError(
                "Episode was accepted but no matching fact appeared before timeout: "
                + json.dumps(last_result)[:500]
            )
        print("Synthetic fact found", flush=True)
    finally:
        episodes = client.tool("get_episodes", {"group_ids": [args.group], "max_episodes": 20})
        episode_id = next(
            (item["uuid"] for item in episodes.get("episodes", []) if item.get("name") == marker),
            None,
        )
        if episode_id:
            client.tool("delete_episode", {"uuid": episode_id})
            print("Synthetic episode deleted", flush=True)
        else:
            print(f"No persisted episode found for {marker}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, urllib.error.URLError, RuntimeError, ValueError) as exc:
        raise SystemExit(str(exc)) from exc
