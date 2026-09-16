#!/usr/bin/env bash
set -euo pipefail

PROD_HOST="${BGT_PROD_HOST:-${1:-}}"
PROD_DB="${BGT_PROD_DB:-${2:-}}"
LOCAL_DEST="${3:-./local-dev/data/bgtags.db}"

if [ -z "$PROD_HOST" ] || [ -z "$PROD_DB" ]; then
    echo "Usage: $0 <prod-host> <prod-db-path> [local-dest]"
    echo "Or set BGT_PROD_HOST and BGT_PROD_DB in environment."
    exit 1
fi

mkdir -p "$(dirname "$LOCAL_DEST")"

echo "==> Fetching safe read-only snapshot of database from $PROD_HOST..."
scp -O "$PROD_HOST:$PROD_DB" "$LOCAL_DEST"
chmod 644 "$LOCAL_DEST"

echo "==> Successfully synced database to $LOCAL_DEST"
ls -lh "$LOCAL_DEST"
