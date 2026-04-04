#!/usr/bin/env python3
"""Fetch all approved guestbook entries and download drawing images.

Writes _data/guestbook.json with image_url pointing to local assets.
Run this before `jekyll build`.
"""

import json
import os
import urllib.request

API_BASE = "https://guestbook.chenna.me"
ROOT = os.path.join(os.path.dirname(__file__), "..")
DATA_FILE = os.path.join(ROOT, "_data", "guestbook.json")
IMAGES_DIR = os.path.join(ROOT, "assets", "guestbook")


def fetch_all_entries():
    entries = []
    page = 1
    while True:
        url = f"{API_BASE}/api/entries?page={page}&per_page=48"
        with urllib.request.urlopen(url) as resp:
            data = json.loads(resp.read())
        entries.extend(data["entries"])
        if page >= data["pagination"]["total_pages"]:
            break
        page += 1
    return entries


def download_image(entry_id, image_url):
    os.makedirs(IMAGES_DIR, exist_ok=True)
    dest = os.path.join(IMAGES_DIR, f"{entry_id}.png")
    if not os.path.exists(dest):
        with urllib.request.urlopen(image_url) as resp:
            with open(dest, "wb") as f:
                f.write(resp.read())
    return f"/assets/guestbook/{entry_id}.png"


def main():
    os.makedirs(os.path.dirname(DATA_FILE), exist_ok=True)
    entries = fetch_all_entries()

    for entry in entries:
        if entry.get("entry_type") == "drawing" and entry.get("image_url"):
            entry["image_url"] = download_image(entry["id"], entry["image_url"])

    with open(DATA_FILE, "w") as f:
        json.dump(entries, f, indent=2)

    print(f"Fetched {len(entries)} guestbook entries")


if __name__ == "__main__":
    main()
