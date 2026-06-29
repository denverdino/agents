#!/usr/bin/env python3
"""
E2B API demo for Substrate-backed sandboxes.

Demonstrates the full sandbox lifecycle via the E2B-compatible REST API:
  create → get → pause → list(paused) → resume → list(running) → delete

Prerequisites:
  1. SandboxSet "substrate-test" with `agents.kruise.io/backend: substrate`
     applied to the cluster (see sandboxset.yaml in this directory).
  2. sandbox-manager running with `--substrate-addr` pointing to the
     Substrate API server, and `--e2b-enable-auth=false` (or a valid API key).

Usage:
  # Auth disabled (default for local dev):
  python e2b_substrate_demo.py --base-url http://localhost:8080

  # With API key:
  python e2b_substrate_demo.py --base-url http://localhost:8080 --api-key <key>

  # With port-forward to in-cluster sandbox-manager:
  kubectl port-forward -n sandbox-system svc/sandbox-manager 8080:8080
  python e2b_substrate_demo.py
"""

import argparse
import json
import sys
import time

import requests


class E2BClient:
    """Minimal E2B REST API client for sandbox lifecycle operations."""

    def __init__(self, base_url: str, api_key: str = ""):
        self.base_url = base_url.rstrip("/")
        self.headers = {"Content-Type": "application/json"}
        if api_key:
            self.headers["X-API-Key"] = api_key

    def _url(self, path: str) -> str:
        return f"{self.base_url}{path}"

    def _check(self, resp: requests.Response, expected: int, action: str):
        if resp.status_code != expected:
            print(f"  FAILED {action}: {resp.status_code} {resp.text}", file=sys.stderr)
            sys.exit(1)

    def create_sandbox(self, template: str, timeout: int = 600) -> dict:
        resp = requests.post(
            self._url("/sandboxes"),
            headers=self.headers,
            json={"templateID": template, "timeout": timeout},
        )
        self._check(resp, 201, "create_sandbox")
        return resp.json()

    def get_sandbox(self, sandbox_id: str) -> dict:
        resp = requests.get(
            self._url(f"/sandboxes/{sandbox_id}"),
            headers=self.headers,
        )
        self._check(resp, 200, "get_sandbox")
        return resp.json()

    def list_sandboxes(self, state: str = "") -> list:
        params = {}
        if state:
            params["state"] = state
        resp = requests.get(
            self._url("/v2/sandboxes"),
            headers=self.headers,
            params=params,
        )
        self._check(resp, 200, "list_sandboxes")
        return resp.json()

    def pause_sandbox(self, sandbox_id: str):
        resp = requests.post(
            self._url(f"/sandboxes/{sandbox_id}/pause"),
            headers=self.headers,
        )
        self._check(resp, 204, "pause_sandbox")

    def resume_sandbox(self, sandbox_id: str, timeout: int = 600) -> dict:
        resp = requests.post(
            self._url(f"/sandboxes/{sandbox_id}/connect"),
            headers=self.headers,
            json={"timeout": timeout},
        )
        if resp.status_code not in (200, 201):
            print(f"  FAILED resume_sandbox: {resp.status_code} {resp.text}", file=sys.stderr)
            sys.exit(1)
        return resp.json()

    def delete_sandbox(self, sandbox_id: str):
        resp = requests.delete(
            self._url(f"/sandboxes/{sandbox_id}"),
            headers=self.headers,
        )
        self._check(resp, 204, "delete_sandbox")


def pp(label: str, data):
    print(f"\n{'='*60}")
    print(f"  {label}")
    print(f"{'='*60}")
    if isinstance(data, (dict, list)):
        print(json.dumps(data, indent=2, default=str))
    else:
        print(data)


def wait_for_state(client: E2BClient, sandbox_id: str, expected: str, timeout_s: int = 60):
    """Poll until sandbox reaches expected state or timeout."""
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        info = client.get_sandbox(sandbox_id)
        if info.get("state") == expected:
            return info
        time.sleep(2)
    print(f"  TIMEOUT waiting for state={expected}", file=sys.stderr)
    sys.exit(1)


def main():
    parser = argparse.ArgumentParser(description="E2B Substrate sandbox lifecycle demo")
    parser.add_argument("--base-url", default="http://localhost:8080",
                        help="sandbox-manager base URL (default: http://localhost:8080)")
    parser.add_argument("--api-key", default="",
                        help="E2B API key (omit if auth is disabled)")
    parser.add_argument("--template", default="substrate-test",
                        help="SandboxSet name to use as template (default: substrate-test)")
    parser.add_argument("--timeout", type=int, default=600,
                        help="Sandbox timeout in seconds (default: 600)")
    args = parser.parse_args()

    client = E2BClient(args.base_url, args.api_key)

    # Step 1: Create
    print("\n>>> Creating sandbox...")
    sandbox = client.create_sandbox(args.template, args.timeout)
    sandbox_id = sandbox["sandboxID"]
    pp("Created sandbox", sandbox)

    try:
        # Step 2: Get
        print("\n>>> Getting sandbox info...")
        info = client.get_sandbox(sandbox_id)
        pp("Sandbox info", info)
        assert info["state"] == "running", f"Expected running, got {info['state']}"

        # Step 3: List running
        print("\n>>> Listing running sandboxes...")
        running = client.list_sandboxes(state="running")
        matching = [s for s in running if s["sandboxID"] == sandbox_id]
        pp(f"Found {len(matching)} matching sandbox(es) in running list", matching)
        assert len(matching) == 1

        # Step 4: Pause
        print("\n>>> Pausing sandbox...")
        client.pause_sandbox(sandbox_id)
        info = wait_for_state(client, sandbox_id, "paused")
        pp("Sandbox after pause", info)

        # Step 5: List paused
        print("\n>>> Listing paused sandboxes...")
        paused = client.list_sandboxes(state="paused")
        matching = [s for s in paused if s["sandboxID"] == sandbox_id]
        pp(f"Found {len(matching)} matching sandbox(es) in paused list", matching)
        assert len(matching) == 1

        # Step 6: Resume (via connect endpoint)
        print("\n>>> Resuming sandbox...")
        resumed = client.resume_sandbox(sandbox_id, args.timeout)
        pp("Sandbox after resume", resumed)
        info = wait_for_state(client, sandbox_id, "running")
        assert info["state"] == "running", f"Expected running after resume, got {info['state']}"

        # Step 7: List running again
        print("\n>>> Listing running sandboxes after resume...")
        running = client.list_sandboxes(state="running")
        matching = [s for s in running if s["sandboxID"] == sandbox_id]
        pp(f"Found {len(matching)} matching sandbox(es) in running list", matching)
        assert len(matching) == 1

        # Step 8: Delete
        print("\n>>> Deleting sandbox...")
        client.delete_sandbox(sandbox_id)
        print("  Sandbox deleted successfully")

        # Verify deleted
        print("\n>>> Verifying sandbox is gone...")
        resp = requests.get(
            f"{args.base_url}/sandboxes/{sandbox_id}",
            headers=client.headers,
        )
        assert resp.status_code == 404, f"Expected 404 after delete, got {resp.status_code}"
        print("  Confirmed: sandbox no longer exists (404)")

        print("\n" + "=" * 60)
        print("  ALL STEPS PASSED")
        print("=" * 60)

    except Exception:
        # Cleanup on failure
        print(f"\n>>> Cleaning up sandbox {sandbox_id}...")
        try:
            client.delete_sandbox(sandbox_id)
        except Exception:
            pass
        raise


if __name__ == "__main__":
    main()
