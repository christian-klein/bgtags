# AI Agent Guide: Adding Games & Rules to `bgtags`

Welcome to the **`bgtags`** repository. This document serves as the canonical technical guide and operating manual for AI agents (and human developers) adding board games, rules, player aids, expansions, and companion materials to the system.

---

## 1. System Overview & Core Capabilities

`bgtags` is a self-hosted web service that bridges physical board games with digital rules and companion resources:
- **Physical QR Code Stickers (`/stickers`)**: Generates printable Avery-style sheets of stickers formatted with game title, box art, min/max/best player count, complexity rating, and a direct QR code linking to the game's rule hub.
- **Dual-Mode Rules Hub (`/games/{id}/rules`)**:
  - **Single Rule**: If a game only has 1 primary document, navigating to `/games/{id}/rules` directly opens or downloads the PDF.
  - **Multi-Rule / Companion Hub**: If a game has multiple documents (e.g. core rules, expansions, player aids, teaching scripts, FAQs), it presents an interactive, mobile-responsive landing page categorized into:
    - 📖 **Core Rules**
    - 🧩 **Expansions**
    - 📋 **Player Aids & References**
    - ❓ **FAQs & Errata**
- **Public & Local Access**: Configured via `BASE_URL` in `.env` (e.g. `https://bgtags.example.com` or direct client fallback).

---

## 2. Infrastructure & Host Architecture

> [!NOTE]
> All infrastructure endpoints, server IPs, and SSH credentials are maintained privately in the `home_automations` repository.

| Component | Host / IP | Location / Path | Purpose |
| :--- | :--- | :--- | :--- |
| **bgtags Application** | `<docker-host>` | Docker container `bgtags` (`:8080`) | Go 1.22 + Templ web application |
| **Live SQLite Database** | `<docker-host>` | `/app/data/bgtags.db` | Operational database |
| **Rules Storage** | `<storage-host>` | Rules volume mounted to `/app/rules/` | Persistent PDF storage |
| **Image Storage** | `<storage-host>` | Images volume mounted to `/app/img/` | Persistent box art |
| **Backup Storage** | `<storage-host>` | Backup volume mounted to `/app/backups/` | Periodic DB & asset snapshots |
| **Seed / Source of Truth** | Git Repo | `data/seed_games.json` | Repository-tracked seed data for fresh deployments |

---

## 3. Database Schema & Data Models

The SQLite database consists of two primary tables:

### 3.1 `games` Table
Stores high-level game metadata:
```sql
CREATE TABLE IF NOT EXISTS games (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    url TEXT NOT NULL,          -- Primary PDF filename (e.g. "stationfall-reference-manual.pdf")
    image TEXT,                 -- Box art filename in img/ (e.g. "stationfall.png")
    min_players INTEGER,
    max_players INTEGER,
    best_players TEXT,          -- Community recommendation (e.g. "5-6", "4", "3-4")
    complexity REAL,            -- BGG average weight float 1.00-5.00 (e.g. 4.09)
    bgg_url TEXT,               -- Full BGG URL (e.g. "https://boardgamegeek.com/boardgame/316624/stationfall")
    parent_id INTEGER,          -- NULL for base games; points to base game ID for standalone expansions
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 3.2 `game_documents` Table
Stores all individual PDF files associated with a game:
```sql
CREATE TABLE IF NOT EXISTS game_documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    title TEXT NOT NULL,        -- Descriptive title (e.g. "Reference Manual", "Expedition Leaders Rules")
    category TEXT NOT NULL,     -- One of: 'core', 'expansion', 'reference', 'faq'
    filename TEXT NOT NULL,     -- Exact PDF filename stored in rules/
    is_primary INTEGER DEFAULT 0, -- 1 for main rulebook, 0 for secondary
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 4. Ground Rules & Content Policies

1. **ENGLISH CONTENT ONLY**:
   - **MANDATORY**: Only download and store English rulebooks, guides, and player aids.
   - Do NOT upload German, French, Spanish, or other international editions unless explicitly requested by the user.
2. **Naming Conventions**:
   - Rulebooks: `kebab-case.pdf` (e.g., `beyond-the-sun.pdf`, `beyond-the-sun-quick-start.pdf`).
   - Images: `kebab-case.jpg` or `kebab-case.png` (e.g., `beyond-the-sun.jpg`).
3. **File Verification**:
   - Check file integrity (`file <filename>` or `pdfinfo <filename>`).
   - Verify extracted text contains English headings (`pdftotext <filename> - | head -n 30`).
4. **File Permissions**:
   - All files copied to Synology DiskStation (`/volume1/docker/bgtags/rules/` and `/img/`) must have read permissions for the container process (`chmod 666` or `chmod 644`).

---

## 5. Step-by-Step Workflow to Add a Game

### Step 1: Select Game & Retrieve Metadata
- Consult `GAMES_TODO.md` (ordered by complexity descending) or `remaining_games_by_complexity.json`.
- Identify the BGG ID and title.
- Required attributes:
  - `name`: Official title (e.g., "Gaia Project")
  - `min_players`, `max_players`: Integer range
  - `best_players`: BGG community best player count string (e.g., "4" or "3-4")
  - `complexity`: BGG weight (1.00 - 5.00)
  - `bgg_url`: Full link to BGG page

### Step 2: Download Rules & Companion Documents
You can source rule PDFs from either:
1. **Official Publisher Websites** (e.g. Stonemaier, CGE, Leder Games, Fantasy Flight):
   ```bash
   curl -sL "https://publisher.com/rules.pdf" -o /tmp/game-rules.pdf
   ```
2. **BoardGameGeek File Page (`https://boardgamegeek.com/boardgame/{bgg_id}/{slug}/files`)**:
   - BGG file downloads are protected behind Cloudflare Turnstile and session cookies.
   - Use the Chrome CDP downloader script: `scripts/bgg_cdp_downloader.mjs`.
   - Syntax:
     ```bash
     node scripts/bgg_cdp_downloader.mjs "<bgg_file_page_url>" "/tmp/target-filename.pdf"
     ```
   - How it works: Connects to the local running Chrome instance (`localhost:9222`), clicks the download button, intercepts the pre-signed AWS S3 URL (`Network.requestWillBeSent`), and streams the PDF directly.

### Step 3: Download Box Art
- Fetch the game's high-res thumbnail/cover image from BGG:
  ```bash
  curl -sL "<bgg_image_url>" -o /tmp/game-title.jpg
  ```

### Step 4: Transfer Assets to Storage (Synology DiskStation)
Transfer the PDFs and image to DiskStation (`10.0.0.11`):
```bash
# Copy PDFs to rules directory
scp -O /tmp/*.pdf cdk2128@10.0.0.11:/volume1/docker/bgtags/rules/

# Copy box art to img directory
scp -O /tmp/*.jpg cdk2128@10.0.0.11:/volume1/docker/bgtags/img/

# Ensure permissions are readable by Docker
ssh cdk2128@10.0.0.11 "chmod 666 /volume1/docker/bgtags/rules/*.pdf /volume1/docker/bgtags/img/*"
```

### Step 5: Update the Live Database (`10.0.0.45`)
Run an SQLite update directly on Docker host `10.0.0.45` using Python or `sqlite3`:

```python
import sqlite3

conn = sqlite3.connect("/usr/src/docker/bgtags/data/bgtags.db")
cur = conn.cursor()

# 1. Insert or update the game
cur.execute("""
    INSERT INTO games (name, url, image, min_players, max_players, best_players, complexity, bgg_url)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
        url=excluded.url, image=excluded.image, min_players=excluded.min_players,
        max_players=excluded.max_players, best_players=excluded.best_players,
        complexity=excluded.complexity, bgg_url=excluded.bgg_url
""", ("Game Title", "game-rules.pdf", "game-title.jpg", 1, 4, "3", 3.75, "https://boardgamegeek.com/boardgame/..."))

game_id = cur.lastrowid or cur.execute("SELECT id FROM games WHERE name = ?", ("Game Title",)).fetchone()[0]

# 2. Insert documents
docs = [
    ("Core Rulebook", "core", "game-rules.pdf", 1),
    ("Quick Reference Sheet", "reference", "game-quick-ref.pdf", 0),
]
cur.execute("DELETE FROM game_documents WHERE game_id = ?", (game_id,))
for title, category, filename, is_primary in docs:
    cur.execute("""
        INSERT INTO game_documents (game_id, title, category, filename, is_primary)
        VALUES (?, ?, ?, ?, ?)
    """, (game_id, title, category, filename, is_primary))

conn.commit()
conn.close()
```

### Step 6: Update Seed File (`data/seed_games.json`)
Always update `data/seed_games.json` in the Git repository to keep development and production in parity:
```json
{
  "name": "Game Title",
  "url": "game-rules.pdf",
  "image": "game-title.jpg",
  "min_players": 1,
  "max_players": 4,
  "best_players": "3",
  "complexity": 3.75,
  "bgg_url": "https://boardgamegeek.com/boardgame/...",
  "documents": [
    {
      "title": "Core Rulebook",
      "category": "core",
      "filename": "game-rules.pdf",
      "is_primary": true
    },
    {
      "title": "Quick Reference Sheet",
      "category": "reference",
      "filename": "game-quick-ref.pdf",
      "is_primary": false
    }
  ]
}
```

### Step 7: Verify Live Endpoints
Test using `curl` or browser:
- Rules Hub endpoint: `http://10.0.0.45:8082/games/{game_id}/rules` (should return HTTP 200 with document cards, or direct redirect if 1 doc).
- Sticker Page: `http://10.0.0.45:8082/stickers` (verify QR code, weight, and player counts).

### Step 8: Update Backlog & Git Commit
- Regenerate or update `GAMES_TODO.md` by running:
  ```bash
  node scripts/extract_remaining_games.mjs
  ```
- Commit and push changes to the active branch (`feature/go-htmx`):
  ```bash
  git add data/seed_games.json GAMES_TODO.md remaining_games_by_complexity.json
  git commit -m "feat(rules): add rules and companion guides for <Game Name>"
  git push origin <branch>
  ```

---

## 6. Helper Scripts Reference

All operational automation is available under `scripts/`:

| Script | Purpose |
| :--- | :--- |
| `scripts/bgg_cdp_downloader.mjs` | Downloads rulebooks from BGG file pages via authenticated Chrome CDP session |
| `scripts/extract_remaining_games.mjs` | Pulls BGG collection via CDP, excludes active DB games, and updates `GAMES_TODO.md` |
| `scripts/fetch_bgg_collection.py` | Python fallback collection parser |

---

## 7. Common Gotchas & Troubleshooting

1. **Cloudflare Block / Captcha on BGG**:
   - Do NOT attempt pure `curl` or unauthenticated scrapers against BGG file download endpoints.
   - Use `scripts/bgg_cdp_downloader.mjs` which utilizes the logged-in Chrome browser session on port `9222`.
2. **S3 Pre-Signed URL Expiration**:
   - BGG's AWS S3 download links expire within 120 seconds. The CDP script streams them immediately upon intercepting `Network.requestWillBeSent`.
3. **HTTP 500 or Empty Rules in Browser**:
   - Check file permissions on Synology DiskStation (`ls -l /volume1/docker/bgtags/rules/`). If files are owned by `root:root` with mode `600`, Docker's unprivileged user cannot serve them. Always run `chmod 666`.
4. **PostgreSQL JIT / Performance**:
   - Irrelevant for `bgtags` (which uses SQLite), but when integrating with Authentik SSO, remember PostgreSQL JIT is disabled (`SET jit = off;`).
