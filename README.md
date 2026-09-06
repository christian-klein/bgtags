<div align="center">
  <img src="static/img/bgtags-icon.png" width="128" height="128" alt="bgtags logo" style="border-radius: 24px;" />
  <h1>bgtags</h1>
  <p><strong>Self-hosted digital rulebook hub & physical QR code sticker generator for board game boxes.</strong></p>
</div>

A fast, lightweight Go + HTMX web application for board game rule tags and scannable QR codes.

## Features
- **Instant Search & Player Filter**: Live filtering powered by HTMX without full-page reloads.
- **Dynamic QR Code Engine**: Generates crisp, scannable QR codes linking directly to rulebooks.
- **Inline Rulebook PDFs**: Serves PDFs inline for fast reading on phones, tablets, or desktop browsers.
- **Game Box Sticker Printing**: Dedicated printable layout (`/stickers`) with scannable QR codes, player counts, and cover art thumbnails for sticking directly onto physical game boxes.
- **SQLite Storage & Automated Backups**: Stores catalog in SQLite with automated daily plain-text JSON backups with rolling retention.
- **Zero-CGO Multi-Stage Docker**: Containerized with pure Go and Alpine for a tiny footprint (~20MB).

## Getting Started

### Local Development (Go)
```bash
# Run tests
go test ./...

# Run server
go run ./cmd/server
```
Visit [http://localhost:8081](http://localhost:8081).

### Running with Docker (Sample)
```bash
# Copy sample configuration
cp docker-compose.sample.yml docker-compose.yml
cp .env.sample .env

# Build and start container
docker compose up -d
```

## Configuration

Configurations can be supplied via environment variables or `.env`:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port for the server |
| `BASE_URL` | *(auto-detected)* | Public URL (used for QR code target links) |
| `DB_PATH` | `data/bgtags.db` | Path to SQLite database file |
| `DATA_DIR` | `data` | Directory containing seed data |
| `STATIC_DIR` | `static` | Directory containing images and rules PDFs |
| `BACKUP_ENABLED` | `true` | Enable automated daily backups |
| `BACKUP_DIR` | `backups` | Directory where JSON backup dumps are written |
| `BACKUP_TIME` | `00:00` | UTC time (HH:MM) to run automated backups |
| `BACKUP_RETENTION` | `30` | Number of daily backups to keep |

## Box Sticker Printing
Navigate to `/stickers` or click **"🏷️ Print Stickers"** in the navigation bar to preview and print box labels. Use your browser's Print dialog with:
- Margins: *None* or *Minimum*
- Background graphics: *Enabled*
