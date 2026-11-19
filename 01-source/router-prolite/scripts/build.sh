#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="${NAME:-ProLite}"
TAGS="desktop,production"
LDFLAGS="-s -w"

case "$(uname -s)" in
  Darwin)
    go build -tags "$TAGS" -ldflags "$LDFLAGS" -o "$NAME" .
    ;;
  Linux)
    if pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
      TAGS="$TAGS,webkit2_41"
    fi
    go build -tags "$TAGS" -ldflags "$LDFLAGS" -o "$NAME" .
    ;;
  *)
    echo "Unsupported platform: $(uname -s)" >&2
    exit 1
    ;;
esac

echo "built: $(pwd)/$NAME"
