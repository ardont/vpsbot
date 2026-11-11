#!/usr/bin/env bash
# ==============================================================================
# install.sh — Полная автоматическая установка SuperBot на VPS (Ubuntu / Debian)
# ==============================================================================

set -euo pipefail

echo "=================================================="
echo "          Установка SuperBot (VPS Hybrid)         "
echo "=================================================="

if [[ $EUID -ne 0 ]]; then
   echo "❌ Ошибка: запуск скрипта требует прав root (sudo)."
   exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "==> [1/6] Настройка ZRAM и параметров ядра..."
bash "$SCRIPT_DIR/setup_zram.sh"

echo "==> [2/6] Настройка cgroups v2 (vpn.slice и ai.slice)..."
bash "$SCRIPT_DIR/setup_cgroups.sh"

echo "==> [3/6] Подготовка рабочей директории /opt/superbot..."
mkdir -p /opt/superbot

if [[ ! -f /opt/superbot/.env ]]; then
    if [[ -f "$PROJECT_ROOT/.env.example" ]]; then
        cp "$PROJECT_ROOT/.env.example" /opt/superbot/.env
        chmod 600 /opt/superbot/.env
        echo "Создан файл /opt/superbot/.env (права 600). Обязательно внесите туда ваши ключи!"
    fi
fi

echo "==> [4/6] Установка бинарников superbot и mcp-telegram..."
if [[ -f "$PROJECT_ROOT/bin/superbot" ]]; then
    install -m 755 "$PROJECT_ROOT/bin/superbot" /usr/local/bin/superbot
elif command -v go >/dev/null 2>&1; then
    echo "Компиляция superbot на сервере..."
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/superbot ./cmd/bot)
fi

if [[ -f "$PROJECT_ROOT/bin/mcp-telegram" ]]; then
    install -m 755 "$PROJECT_ROOT/bin/mcp-telegram" /usr/local/bin/mcp-telegram-bridge
elif command -v go >/dev/null 2>&1; then
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/mcp-telegram-bridge ./cmd/mcp-telegram)
fi

# Установка Google MCP
if [[ -f "$SCRIPT_DIR/setup_google_mcp.sh" ]]; then
    bash "$SCRIPT_DIR/setup_google_mcp.sh" || true
fi

echo "==> [5/6] Установка и регистрация systemd-сервиса..."
cp "$SCRIPT_DIR/superbot.service" /etc/systemd/system/superbot.service
systemctl daemon-reload
systemctl enable superbot.service

echo "==> [6/6] Готово!"
echo "--------------------------------------------------"
echo "1. Отредактируйте файл с секретами:"
echo "   nano /opt/superbot/.env"
echo "2. Запустите сервис:"
echo "   systemctl start superbot"
echo "3. Проверьте логи:"
echo "   journalctl -u superbot -f"
echo "--------------------------------------------------"

