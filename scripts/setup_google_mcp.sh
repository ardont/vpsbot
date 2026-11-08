#!/usr/bin/env bash
# ==============================================================================
# setup_google_mcp.sh — Автоматическая загрузка и настройка google-mcp (Go)
# Gmail & Google Calendar MCP Server для Antigravity
# ==============================================================================

set -euo pipefail

echo "=================================================="
echo "      Настройка Google Workspace MCP (Go)         "
echo "=================================================="

INSTALL_DIR="/opt/superbot"
BINARY_NAME="google-mcp-linux-amd64"
TARGET_PATH="$INSTALL_DIR/$BINARY_NAME"
LATEST_URL="https://github.com/shotah/google-mcp/releases/latest/download/$BINARY_NAME"

mkdir -p "$INSTALL_DIR"

echo "==> [1/3] Загрузка бинарника google-mcp (Linux AMD64)..."
if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$LATEST_URL" -o "$TARGET_PATH"
elif command -v wget >/dev/null 2>&1; then
    wget -q "$LATEST_URL" -O "$TARGET_PATH"
else
    echo "❌ Ошибка: не найден curl или wget."
    exit 1
fi

chmod +x "$TARGET_PATH"
echo "✅ Бинарник успешно сохранен в $TARGET_PATH"

echo "==> [2/3] Проверка переменных окружения..."
if [[ -f "$INSTALL_DIR/.env" ]]; then
    set -a
    source "$INSTALL_DIR/.env"
    set +a
fi

if [[ -z "${GOOGLE_CLIENT_ID:-}" ]] || [[ "${GOOGLE_CLIENT_ID:-}" == *"your-client-id"* ]]; then
    echo "⚠️ Внимание: GOOGLE_CLIENT_ID не настроен в $INSTALL_DIR/.env"
    echo "Получите Client ID и Client Secret в Google Cloud Console:"
    echo "https://console.cloud.google.com/apis/credentials"
    echo "Затем запустите авторизацию вручную: $TARGET_PATH auth"
    exit 0
fi

echo "==> [3/3] Авторизация OAuth 2.0..."
echo "Для прохождения авторизации запустите:"
echo "   $TARGET_PATH auth"
echo "После перехода по ссылке и подтверждения доступа токен сохранится в $INSTALL_DIR"
