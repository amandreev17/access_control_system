#!/bin/bash
# Тонкая обёртка: вся логика запуска — в serve.js
# (кроссплатформенно: Windows / macOS / Linux, без WSL, bash, python и openssl).
#
# Использование:  ./serve.sh      или   node serve.js      или   npm run serve
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"

if ! command -v node >/dev/null 2>&1; then
    echo "❌ Node.js не найден."
    echo "   Установите Node.js (LTS): https://nodejs.org/"
    exit 1
fi

exec node "$DIR/serve.js" "$@"
