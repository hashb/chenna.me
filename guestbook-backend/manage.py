#!/usr/bin/env python3
"""Guestbook management CLI.

Usage:
  python manage.py list                  # list pending entries
  python manage.py approved [--page N]   # list approved entries
  python manage.py approve <id>          # approve a pending entry
  python manage.py reject <id>           # reject a pending entry
  python manage.py delete <id>           # delete any entry
  python manage.py purge                 # purge all rejected entries
  python manage.py search <name>         # search approved entries by name

Token resolution order:
  1. --token flag
  2. ADMIN_TOKEN environment variable
  3. fly ssh (auto-fetched from the running machine)
"""

import argparse
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

BASE_URL = "https://chenna-guestbook.fly.dev"
FLY_APP = "chenna-guestbook"


def get_token(explicit=None):
    if explicit:
        return explicit
    token = os.environ.get("ADMIN_TOKEN")
    if token:
        return token
    print("ADMIN_TOKEN not set, fetching from fly...", file=sys.stderr)
    try:
        result = subprocess.run(
            ["fly", "ssh", "console", "-C", "printenv ADMIN_TOKEN", "--app", FLY_APP],
            capture_output=True, text=True, timeout=30,
        )
        # Output has a "Connecting to ..." line before the actual value
        lines = [l.strip() for l in result.stdout.splitlines() if l.strip() and not l.startswith("Connecting")]
        if lines:
            return lines[-1]
    except FileNotFoundError:
        print("fly CLI not found.", file=sys.stderr)
    except subprocess.TimeoutExpired:
        print("Timed out fetching token from fly.", file=sys.stderr)
    print("Error: could not determine ADMIN_TOKEN.", file=sys.stderr)
    sys.exit(1)


def request(method, path, token=None):
    req = urllib.request.Request(BASE_URL + path, method=method)
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        try:
            body = json.loads(e.read())
            msg = body.get("error", "unknown error")
        except Exception:
            msg = str(e)
        print(f"HTTP {e.code}: {msg}", file=sys.stderr)
        sys.exit(1)


def fmt_entry(e, show_status=False):
    status = f" [{e['status']}]" if show_status else ""
    website = f"  {e['website']}" if e.get("website") else ""
    lines = [f"  id={e['id']}  {e['name']}{website}{status}  ({e['entry_type']}, {e['created_at'][:10]})"]
    if e.get("content"):
        preview = e["content"][:100].replace("\n", " ")
        if len(e["content"]) > 100:
            preview += "..."
        lines.append(f"    {preview}")
    return "\n".join(lines)


# --- commands ---

def cmd_list(args, token):
    data = request("GET", "/api/admin/entries", token)
    entries = data["entries"]
    if not entries:
        print("No pending entries.")
        return
    print(f"{len(entries)} pending:")
    for e in entries:
        print(fmt_entry(e, show_status=True))


def cmd_approved(args, token):
    page = args.page
    data = request("GET", f"/api/entries?page={page}&per_page=48")
    p = data["pagination"]
    entries = data["entries"]
    print(f"Page {p['page']}/{p['total_pages']}  ({p['total_entries']} total approved)")
    if not entries:
        print("  (empty)")
        return
    for e in entries:
        print(fmt_entry(e))


def cmd_approve(args, token):
    data = request("POST", f"/api/admin/entries/{args.id}/approve", token)
    print(data["message"])


def cmd_reject(args, token):
    data = request("POST", f"/api/admin/entries/{args.id}/reject", token)
    print(data["message"])


def cmd_delete(args, token):
    data = request("DELETE", f"/api/admin/entries/{args.id}", token)
    print(data["message"])


def cmd_purge(args, token):
    data = request("POST", "/api/admin/purge-rejected", token)
    print(f"{data['message']} ({data['deleted']} deleted)")


def cmd_search(args, token):
    needle = args.name.lower()
    page, found = 1, 0
    while True:
        data = request("GET", f"/api/entries?page={page}&per_page=48")
        for e in data["entries"]:
            if needle in e["name"].lower() or needle in e.get("content", "").lower():
                print(fmt_entry(e))
                found += 1
        if page >= data["pagination"]["total_pages"]:
            break
        page += 1
    if not found:
        print(f"No approved entries matching '{args.name}'.")
    else:
        print(f"\n{found} result(s).")


# --- main ---

def main():
    parser = argparse.ArgumentParser(description="Guestbook admin CLI")
    parser.add_argument("--token", help="Admin token (overrides env/fly)")
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("list", help="List pending entries")

    p_approved = sub.add_parser("approved", help="List approved entries")
    p_approved.add_argument("--page", type=int, default=1)

    p_approve = sub.add_parser("approve", help="Approve a pending entry")
    p_approve.add_argument("id", type=int)

    p_reject = sub.add_parser("reject", help="Reject a pending entry")
    p_reject.add_argument("id", type=int)

    p_delete = sub.add_parser("delete", help="Delete any entry")
    p_delete.add_argument("id", type=int)

    sub.add_parser("purge", help="Purge all rejected entries")

    p_search = sub.add_parser("search", help="Search approved entries by name/content")
    p_search.add_argument("name")

    args = parser.parse_args()

    needs_token = args.cmd not in ("approved", "search")
    token = get_token(args.token) if needs_token else args.token

    {
        "list": cmd_list,
        "approved": cmd_approved,
        "approve": cmd_approve,
        "reject": cmd_reject,
        "delete": cmd_delete,
        "purge": cmd_purge,
        "search": cmd_search,
    }[args.cmd](args, token)


if __name__ == "__main__":
    main()
