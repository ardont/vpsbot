#!/usr/bin/env bash
# ==============================================================================
# setup_zram.sh — Настройка ZRAM (1 ГБ, zstd, приоритет 100) + sysctl оптимизация
# Для VPS с 2 ГБ ОЗУ (Ubuntu / Debian)
# ==============================================================================

set -euo pipefail

echo "==> [1/4] Проверка прав root..."
if [[ $EUID -ne 0 ]]; then
   echo "❌ Этот скрипт должен быть запущен с правами root (sudo)."
   exit 1
fi

echo "==> [2/4] Установка zram-tools..."
apt-get update -qq
apt-get install -y -qq zram-tools

echo "==> [3/4] Конфигурация /etc/default/zramswap..."
cat << 'EOF' > /etc/default/zramswap
# Размер ZRAM пула: 1024 МБ (1 ГБ)
SIZE=1024
# Быстрый современный алгоритм сжатия (коэффициент ~2.7x)
ALGO=zstd
# Приоритет выше обычного дискового свопа (100 vs 10)
PRIORITY=100
EOF

systemctl restart zramswap
systemctl enable zramswap

echo "==> [4/4] Оптимизация параметров ядра Linux (sysctl)..."
cat << 'EOF' > /etc/sysctl.d/99-superbot-memory.conf
# Агрессивно сжимать неактивные страницы в быстрый ZRAM
vm.swappiness=150
# Удерживать кэши файловой системы в памяти
vm.vfs_cache_pressure=50
EOF

sysctl --system > /dev/null

echo "✅ ZRAM успешно настроен и активирован!"
echo "--- Текущее состояние пула ZRAM ---"
zramctl || true

