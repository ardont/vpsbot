# 🤖 SuperBot — VPS Hybrid Admin & AI Bridge Daemon

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20AMD64-FCC624?style=flat&logo=linux)](https://ubuntu.com)
[![Architecture](https://img.shields.io/badge/Architecture-Hybrid%20AI%20%2B%20Sysadmin-6f42c1?style=flat)]()
[![Security](https://img.shields.io/badge/Auth-Whitelist%20%2B%20Invites-success?style=flat)]()
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**SuperBot** — высокопроизводительный гибридный демон на Go для комплексного мониторинга, безопасности и администрирования VPS-сервера (Debian/Ubuntu, 1 vCPU, 2 ГБ ОЗУ) через Telegram с двусторонней интеграцией с ИИ-агентом **Google Antigravity**.

Демон объединяет прямое управление ядром Linux (cgroups v2, sysinfo, fail2ban, iptables) и мост нового поколения **agy-pilot** с живым стримингом шагов выполнения задач нейросетью.

---

## 🏛 Архитектура системы

```text
               +--------------------------------------------------+
               |                  Telegram User                   |
               +--------------------------------------------------+
                                        ▲
                                        │ Telegram Bot API (Long-polling / Webhooks)
                                        ▼
+---------------------------------------------------------------------------------+
|                                    SUPERBOT                                     |
|  - Router & Dispatcher (Commands, Inline Buttons, Persistent Reply Keyboard)    |
|  - Security & Whitelist (users.json, Invite system, Role-based Access)          |
|  - System Diagnostics & Formatting (Progress bars, Markdown-to-HTML parser)     |
|  - Cgroups v2 & Systemd Supervisor (Mode Switcher, VPN restarter, Drop Caches)  |
|  - Session Persistence Manager (sessions.json)                                  |
|  - Local REST API Server (http://127.0.0.1:43428)                               |
+---------------------------------------------------------------------------------+
          │                                                    │
          │ HTTP /api/send                                     │ HTTP NDJSON Stream (:43430)
          ▼                                                    ▼
+---------------------+                             +---------------------+
| Local Scripts / CLI |                             |      AGY-PILOT      |
| (make_backup.sh,    |                             | (Go Bridge Daemon)  |
| check_mail.py, OCR) |                             +---------------------+
+---------------------+                                        │
                                                               │ AgentAPI / LS Socket
                                                               ▼
                                                    +---------------------+
                                                    |  Google Antigravity |
                                                    |  (AI Engine / Hub)  |
                                                    +---------------------+
```

---

## 🌟 Ключевые возможности

### 1. 🕹 Интерактивное меню и навигация
* **Главное меню:** быстрый доступ к статусу, почте, сервисам, очистке RAM, перезапуску VPN, переключению режимов и ИИ.
* **🌐 Меню сервисов:** прямой переход к веб-панелям по Tailscale IP (`100.89.7.20`):
  * `📁 FileBrowser`: `http://100.89.7.20:8080` (файловый менеджер и бэкапы)
  * `🌐 OmniRoute`: `http://100.89.7.20:20128` (шлюз и роутинг нейросетей)
  * `🔒 Панель 3X-UI`: `http://100.89.7.20:53395` (управление VPN и клиентами)
  * `🔑 Antigravity IDE`: `https://vm-576151.tailb9b76e.ts.net:43429` (веб-интерфейс разработки)
* **🖥 Меню сервера:** мгновенный статус за 0.05 сек, очистка RAM (`drop_caches`), рестарт VPN, топы памяти и CPU, лог системных ошибок.
* **🛡 Меню безопасности:** аварийный Kill Switch для SSH, список забаненных IP, кнопка «Разбанить всех», открытые порты, лог Fail2ban.
* **🌐 Меню Сети & VPN:** внешний и Tailscale IP, пинг-тест, перезапуск `x-ui` и `xray`, статистика сетевых интерфейсов.
* **⌨️ Постоянная клавиатура (Reply Keyboard):** всегда видимая нижняя сетка кнопок (3×3) для мгновенного управления со смартфона.

### 2. 🧠 Мост к ИИ Antigravity (`agy-pilot`)
* **Живой стриминг шагов (Live Execution Feed):** бот динамически редактирует статусное сообщение в Telegram (с троттлингом 1.6 сек), показывая текущие шаги, команды и сниппеты вывода в реальном времени.
* **Персистентность сессий (`sessions.json`):** контекст диалога сохраняется на диске и восстанавливается при перезапуске демона или сервера.
* **Защита от сброса сессий:** ошибки или сетевые задержки больше не стирают активную сессию пользователя. Сброс контекста происходит только по явной команде (`/reset` или кнопка «🔄 Новый диалог»).
* **Расширенный таймаут:** лимит выполнения сложных многошаговых задач увеличен до 40 минут с адаптивным контролем активности (heartbeat).
* **Контекстные ответы и цитаты:** поддержка нативных Telegram Replies и цитирования сообщений — бот передает цитируемый контекст модели.
* **Анализ фото и документов:** автоматическое сохранение изображений и документов, оптическое распознавание текста (OCR) и передача контекста в модель.
* **Режим Verbose:** переключатель краткого и подробного вывода выполненных шагов в меню ИИ.

### 3. ⚙️ Управление ресурсами ядра (cgroups v2)
* **Режим «Турбо VPN» (`/mode vpn`):** служба `x-ui.service` получает повышенный приоритет CPU и увеличенный лимит памяти. При этом ИИ защищен от голодания (`CPUWeight=250`, `MemoryHigh=350M`).
* **Режим «Супер-ИИ» (`/mode ai`):** службы Antigravity и OmniRoute получают до 80% квантов процессора.
* **Сбалансированный режим (`/mode auto`):** паритетное распределение ресурсов между VPN и ИИ (50/50).
* **Очистка RAM (`act_drop_caches`):** выполнение системной команды `sync; echo 3 > /proc/sys/vm/drop_caches` с наглядным отчетом об освобожденных мегабайтах (MiB).
* **Перезапуск VPN:** одновременный и безопасный рестарт служб `systemctl restart x-ui` и `systemctl restart xray` со всплывающим Toast-уведомлением.

### 4. 🔒 Безопасность и разграничение доступа
* **Белый список и система инвайтов:** защита от несанкционированного доступа. Новые пользователи могут зарегистрироваться только по одноразовым кодам (`/invite`) или через одобрение администратора (`/adduser`).
* **Аварийный выключатель SSH (Kill Switch):** мгновенная блокировка входящего SSH-трафика (порт 22) одной кнопкой через iptables/UFW при сохранении работы VPN и Telegram-бота.
* **Интеграция с Fail2ban:** мониторинг заблокированных адресов и снятие банов в один клик.

### 5. 📬 Инспекция почты (Gmail + Mail.ru)
* Параллельная проверка ящиков через IMAP (`/opt/superbot/check_mail.py`) за ~2 секунды.
* Поддержка двух режимов: быстрый дайджест непрочитанных писем и глубокий анализ ИИ (выделение вакансий, хакатонов, дедлайнов и важных уведомлений).

### 6. 📦 Умные бэкапы и передача файлов
* **Скрипт `make_backup.sh`:** создание компактного архива с исключением тяжелых бинарников, `.git`, `node_modules` и кэшей.
* **Интеграция с FileBrowser:** постоянный `bind mount` между `/opt/filebrowser/backups` и `/root/backups` (зафиксирован в `/etc/fstab`).
* **Прямая отправка в Telegram:** команда `/send <путь>` отправляет файлы и папки (с автоупаковкой в ZIP) размером до 50 МБ прямо в диалог.

---

## ⚡ Справочник команд

### 📊 Системный мониторинг
| Команда | Описание |
| :--- | :--- |
| `/stats` | Полный дайджест: CPU Load, ОЗУ, Swap/ZRAM, Диск, Uptime с прогресс-барами |
| `/top` | Топ-5 процессов по потреблению оперативной памяти (RSS) |
| `/net` | Внешний IP, Tailscale IP, статус интерфейсов и сетевой трафик |
| `/services` | Список веб-панелей сервера для быстрого перехода |
| `/mail` | Сводка входящих писем Gmail и Mail.ru |
| `/drop_caches` | Очистка неактивного кэша оперативной памяти |
| `/send <путь>` | Скачать файл или папку с сервера в Telegram (до 50 МБ) |
| `/ls <путь>` | Просмотр содержимого каталога с размерами файлов |
| `/find <маска>` | Быстрый поиск файлов с защитой от зависания |

### 👑 Административные команды
| Команда | Описание |
| :--- | :--- |
| `/vpn_restart` | Перезапуск служб `x-ui` и `xray` |
| `/ai_restart` | Перезапуск службы нейросетевого шлюза `omniroute` |
| `/mode <vpn\|ai\|auto>` | Переключение приоритетов cgroups v2 |
| `/invite [часы]` | Создать одноразовый инвайт-код (по умолчанию 24 ч.) |
| `/adduser <id> [имя]` | Добавить пользователя в белый список |
| `/deluser <id>` | Отозвать доступ у пользователя |
| `/users` | Список авторизованных пользователей и их ролей |

### 🧠 ИИ-агент (Antigravity)
* **Обычный текст без слэша:** отправка запроса ИИ-агенту (с живым отображением хода мыслей и вызовов инструментов).
* `/reset` или `/new`: очистить контекст и начать новый диалог.

---

## 🌐 Внутренний REST API (`:43428`)

Демон SuperBot предоставляет локальный HTTP API на порту `127.0.0.1:43428` для отправки сообщений, файлов и интерактивных кнопок из локальных скриптов и Antigravity:

### Отправка сообщения с кнопками (`POST /api/send`)
```bash
curl -s -X POST http://127.0.0.1:43428/api/send \
  -H "Content-Type: application/json" \
  -d '{
    "chat_id": 5342950903,
    "text": "🎉 <b>Задача выполнена!</b>\nВыберите следующее действие:",
    "buttons": [
      [
        {"text": "🔄 Рестарт VPN", "data": "act_restart_vpn"},
        {"text": "🧹 Очистить RAM", "data": "act_drop_caches"}
      ],
      [
        {"text": "🌐 Панель 3X-UI", "url": "http://100.89.7.20:53395"}
      ]
    ]
  }'
```

### Отправка файла в Telegram (`POST /api/send`)
```bash
curl -s -X POST http://127.0.0.1:43428/api/send \
  -H "Content-Type: application/json" \
  -d '{
    "chat_id": 5342950903,
    "text": "📦 Свежий бэкап сервера:",
    "file_path": "/opt/filebrowser/backups/backup_2026-11-11.zip"
  }'
```

---

## 🚀 Установка и сборка

### 1. Клонирование репозитория
```bash
git clone https://github.com/ardont/vpsbot.git /opt/superbot-src
cd /opt/superbot-src
```

### 2. Сборка бинарников
```bash
# Сборка SuperBot
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/superbot ./cmd/bot

# Сборка Antigravity Bridge (agy-pilot)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/agy-pilot ./cmd/agy-pilot
```

### 3. Конфигурация окружения (`.env`)
Создайте файл `/opt/superbot/.env` с правами `chmod 600`:
```ini
TG_BOT_TOKEN=1234567890:ABCdefGHIjklMNOpqrsTUVwxyz
TG_ADMIN_ID=5342950903
ANTIGRAVITY_API_URL=http://127.0.0.1:43430

# Почтовые ящики для check_mail.py
GMAIL_USER=your_email@gmail.com
GMAIL_APP_PASSWORD=your_gmail_app_password
MAILRU_USER=your_email@mail.ru
MAILRU_APP_PASSWORD=your_mailru_app_password
```

### 4. Управление службами systemd
```bash
sudo systemctl restart superbot
sudo systemctl restart agy-pilot

# Проверка статуса
sudo systemctl status superbot agy-pilot
```

### 5. Запуск тестов
```bash
go test -v ./...
```

---

## 📄 Лицензия

Проект распространяется под лицензией [MIT](LICENSE).
Автор: [@ardont](https://github.com/ardont)
