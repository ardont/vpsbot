# 🔌 Настройка MCP-серверов для Antigravity

В данном каталоге собраны конфигурации Model Context Protocol (MCP) для подключения внешних сервисов к агенту Antigravity на VPS.

---

## 1. Google Workspace MCP (`google-mcp`)
Репозиторий: [shotah/google-mcp](https://github.com/shotah/google-mcp)

Дает агенту доступ к **Gmail** (чтение, поиск, отправка писем) и **Google Calendar** (просмотр и создание событий).

### Установка на сервере:
```bash
# 1. Скачать бинарник под Linux AMD64
wget https://github.com/shotah/google-mcp/releases/latest/download/google-mcp-linux-amd64 -O /opt/superbot/google-mcp-linux-amd64
chmod +x /opt/superbot/google-mcp-linux-amd64

# 2. Получить OAuth Client ID и Client Secret в Google Cloud Console:
# - Перейдите в https://console.cloud.google.com/
# - Создайте проект, включите "Gmail API" и "Google Calendar API"
# - В разделе "Credentials" создайте OAuth 2.0 Client ID (тип: Desktop или Web Application)
# - Добавьте Redirect URI: http://localhost:4100/oauth2callback

# 3. Авторизация (создаст токен token.json):
export GOOGLE_CLIENT_ID="ваш_client_id"
export GOOGLE_CLIENT_SECRET="ваш_client_secret"
/opt/superbot/google-mcp-linux-amd64 auth
```

---

## 2. Telegram MCP (Отправка файлов и сообщений)

Доступно два варианта интеграции:

### Вариант А: MTProto User Client ([Prgebish/mcp-telegram](https://github.com/Prgebish/mcp-telegram))
Позволяет агенту действовать **от имени вашего личного Telegram-аккаунта** (отправлять файлы, читать чаты, писать в личку).

#### Установка:
```bash
# Требуется Go 1.21+
go install github.com/Prgebish/mcp-telegram@latest
```

#### Настройка:
1. Перейдите на [my.telegram.org](https://my.telegram.org/) и войдите под своим номером телефона.
2. Откройте **API development tools** и создайте приложение.
3. Скопируйте `api_id` (`TG_APP_ID`) и `api_hash` (`TG_APP_HASH`).
4. При первом запуске выполните вход через терминал:
   ```bash
   export TG_APP_ID="ваш_api_id"
   export TG_APP_HASH="ваш_api_hash"
   mcp-telegram
   ```
   Утилита запросит номер телефона и SMS-код для создания файла сессии `.session`.

---

### Вариант Б: Легковесный SuperBot Bridge (`cmd/mcp-telegram`)
Наш встроенный MCP-сервер, отправляющий файлы и текст **через Telegram Bot API** (порт `:43428`).

* **Преимущества:**
  * Не требует ввода номера телефона и SMS.
  * Нулевой риск блокировки личного Telegram-аккаунта.
  * Потребляет всего **~5 МБ памяти**.
  * Работает полностью локально через демона `superbot`.

#### Установка:
```bash
cp /opt/superbot-src/bin/mcp-telegram /usr/local/bin/mcp-telegram-bridge
chmod +x /usr/local/bin/mcp-telegram-bridge
```

Инструмент `tg_send` автоматически появится в арсенале Antigravity!

---

## 3. Подключение к Antigravity (`mcp_config.json`)

Скопируйте `mcp_config.json` в рабочую конфигурацию Antigravity (обычно `~/.gemini/antigravity/mcp_config.json` или в корень проекта):

```json
{
  "mcpServers": {
    "google": {
      "command": "/opt/superbot/google-mcp-linux-amd64",
      "args": ["--preset", "everyday"],
      "env": {
        "GOOGLE_CLIENT_ID": "${GOOGLE_CLIENT_ID}",
        "GOOGLE_CLIENT_SECRET": "${GOOGLE_CLIENT_SECRET}",
        "GOOGLE_REDIRECT_URI": "http://localhost:4100/oauth2callback"
      }
    },
    "telegram": {
      "command": "mcp-telegram",
      "env": {
        "TG_APP_ID": "${TG_APP_ID}",
        "TG_APP_HASH": "${TG_APP_HASH}"
      }
    }
  }
}
```

