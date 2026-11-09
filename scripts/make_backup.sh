#!/usr/bin/env bash
set -euo pipefail

BACKUP_DIR="/opt/filebrowser/backups"
mkdir -p "$BACKUP_DIR"

# Удаляем остаточные временные файлы zip
rm -f "$BACKUP_DIR"/zi* 2>/dev/null || true
rm -f /root/backups/backup_*.zip 2>/dev/null || true

# Формируем имя файла бэкапа с текущей датой
ZIP_NAME="backup_$(date +%F).zip"
ZIP_PATH="$BACKUP_DIR/$ZIP_NAME"

# Удаляем предыдущий файл за сегодня, чтобы гарантировать чистое пересоздание
rm -f "$ZIP_PATH"

echo "==> Создание чистого системного бэкапа в $ZIP_PATH..."

# Архивация с исключениями кэшей, модулей и тяжелых бинарников
zip -q -r "$ZIP_PATH" \
  /root/vpsbot \
  /opt/superbot \
  /etc/systemd/system \
  /etc/sysctl.d \
  /etc/x-ui \
  -x "*/node_modules/*" \
     "*/logs/*" \
     "*.log" \
     "*/.git/*" \
     "*root/go/*" \
     "*/go/*" \
     "*/.npm/*" \
     "*/.cache/*" \
     "*/__pycache__/*" \
     "*.pyc" \
     "*/backups/*" \
     "*/agy-pilot/agy-pilot" \
     "*/google-mcp"

# Вывод информации о созданном файле
if [[ -f "$ZIP_PATH" ]]; then
    ls -lh "$ZIP_PATH"
else
    echo "❌ Ошибка: файл архива не создан" >&2
    exit 1
fi
