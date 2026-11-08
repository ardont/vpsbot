#!/usr/bin/env bash
# ==============================================================================
# setup_google_mcp.sh — Настройка google-mcp (Go)
# Gmail & Google Calendar MCP Server для Antigravity
# ==============================================================================

set -euo pipefail

INSTALL_DIR="/opt/superbot"
BINARY_PATH="$INSTALL_DIR/google-mcp"

echo "==> [1/2] Проверка наличия бинарника google-mcp..."
if [[ ! -f "$BINARY_PATH" ]]; then
    echo "Загрузка бинарника google-mcp..."
    curl -fsSL https://github.com/shotah/google-mcp/releases/download/v1.0.19/google-mcp_1.0.19_linux_amd64.tar.gz | tar -xz -C "$INSTALL_DIR"
    chmod +x "$BINARY_PATH"
fi

echo "✅ Бинарник готов: $BINARY_PATH"

echo "==> [2/2] Проверка credentials.json..."
if [[ -f "$INSTALL_DIR/credentials.json" ]]; then
    echo "✅ Файл $INSTALL_DIR/credentials.json присутствует."
fi
