#!/usr/bin/env python3
"""
fetch_bgg_collection.py

Downloads a user's board game collection from BoardGameGeek and outputs
a JSON file of all games ordered by BGG Geek Rating (highest to lowest).

Usage:
    python3 scripts/fetch_bgg_collection.py
    python3 scripts/fetch_bgg_collection.py --username Rohirrim70 --output bgg_games.json
    python3 scripts/fetch_bgg_collection.py --only-owned
"""

import argparse
import json
import os
import re
import sys
from bs4 import BeautifulSoup
from curl_cffi import requests


def fetch_collection(username: str, only_owned: bool = False, base_games_only: bool = False):
    url = f"https://boardgamegeek.com/collection/user/{username}?objecttype=thing&ff=1&subtype=boardgame"
    print(f"[*] Fetching collection for '{username}' from {url}...")

    try:
        resp = requests.get(url, impersonate="chrome124", timeout=30)
    except Exception as e:
        print(f"[!] Network error fetching collection: {e}", file=sys.stderr)
        sys.exit(1)

    if resp.status_code != 200:
        print(f"[!] BGG returned HTTP {resp.status_code}", file=sys.stderr)
        sys.exit(1)

    soup = BeautifulSoup(resp.text, "html.parser")
    table = soup.find("table", class_="collection_table")
    if not table:
        print("[!] Could not locate collection_table in BGG HTML response", file=sys.stderr)
        sys.exit(1)

    rows = table.find_all("tr", id=lambda x: x and x.startswith("row_"))
    print(f"[*] Found {len(rows)} items in collection table")

    games = []
    for r in rows:
        name_cell = r.find("td", class_="collection_objectname")
        if not name_cell:
            continue

        link = name_cell.find("a", href=re.compile(r"/(?:boardgame|boardgameexpansion)/\d+"))
        if not link:
            continue

        name = link.get_text(strip=True)
        href = link.get("href", "")
        m = re.search(r"/(boardgame(?:expansion)?)/(\d+)", href)
        subtype = m.group(1) if m else "boardgame"
        bgg_id = int(m.group(2)) if m else None

        is_expansion = (subtype == "boardgameexpansion")
        if base_games_only and is_expansion:
            continue

        year_span = name_cell.find("span", class_="smallerfont")
        year = year_span.get_text(strip=True).strip("()") if year_span else ""
        try:
            year_val = int(year)
        except ValueError:
            year_val = None

        geek_rating_cell = r.find("td", class_="collection_bggrating")
        geek_rating_str = geek_rating_cell.get_text(strip=True) if geek_rating_cell else ""
        try:
            geek_rating = float(geek_rating_str)
        except ValueError:
            geek_rating = 0.0

        user_rating_cell = r.find("td", class_="collection_rating")
        user_rating_str = user_rating_cell.get_text(strip=True) if user_rating_cell else ""
        user_rating_val = None
        if user_rating_str and not user_rating_str.startswith("N/A"):
            num_m = re.match(r"^([0-9]+(?:\.[0-9]+)?)", user_rating_str)
            if num_m:
                try:
                    user_rating_val = float(num_m.group(1))
                except ValueError:
                    pass

        status_cell = r.find("td", class_="collection_status")
        status = status_cell.get_text(strip=True) if status_cell else ""

        if only_owned and not status.startswith("Owned"):
            continue

        plays_cell = r.find("td", class_="collection_plays")
        plays_str = plays_cell.get_text(strip=True) if plays_cell else ""
        try:
            plays = int(plays_str)
        except ValueError:
            plays = 0

        games.append({
            "bgg_id": bgg_id,
            "name": name,
            "year": year_val,
            "geek_rating": geek_rating,
            "user_rating": user_rating_val,
            "status": status,
            "plays": plays,
            "subtype": subtype,
            "is_expansion": is_expansion,
            "bgg_url": f"https://boardgamegeek.com{href}"
        })

    # Order by geek_rating descending (highest rating first)
    games.sort(key=lambda g: g["geek_rating"], reverse=True)
    return games


def main():
    parser = argparse.ArgumentParser(
        description="Download BGG user collection to JSON ordered by Geek Rating."
    )
    parser.add_argument(
        "--username", "-u",
        default="Rohirrim70",
        help="BGG username to fetch (default: Rohirrim70)"
    )
    parser.add_argument(
        "--output", "-o",
        default="bgg_games.json",
        help="Output JSON filename or path (default: bgg_games.json)"
    )
    parser.add_argument(
        "--only-owned",
        action="store_true",
        help="Only include games marked as 'Owned'"
    )
    parser.add_argument(
        "--base-games-only",
        action="store_true",
        help="Filter out expansions and include only base board games"
    )

    args = parser.parse_args()

    games = fetch_collection(
        username=args.username,
        only_owned=args.only_owned,
        base_games_only=args.base_games_only
    )

    # Determine output path (if relative, write relative to cwd or repo root)
    output_path = os.path.abspath(args.output)
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(games, f, indent=2, ensure_ascii=False)

    print(f"[+] Successfully exported {len(games)} games to {output_path}")
    if games:
        print("\nTop 5 games by Geek Rating:")
        for i, g in enumerate(games[:5], 1):
            print(f"  {i}. {g['name']} ({g['year']}) - Geek Rating: {g['geek_rating']}")


if __name__ == "__main__":
    main()
