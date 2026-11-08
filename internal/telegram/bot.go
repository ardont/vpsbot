package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"vpsbot/internal/antigravity"
	"vpsbot/internal/cgroups"
	"vpsbot/internal/config"
	"vpsbot/internal/sysinfo"
)

// Bot manages Telegram updates and command routing.
type Bot struct {
	api    *tgbotapi.BotAPI
	cfg    *config.Config
	sys    *sysinfo.SysInfo
	cg     *cgroups.Manager
	aiCli  *antigravity.Client
}

// New creates a new Telegram bot router.
func New(
	api *tgbotapi.BotAPI,
	cfg *config.Config,
	sys *sysinfo.SysInfo,
	cg *cgroups.Manager,
	aiCli *antigravity.Client,
) *Bot {
	return &Bot{
		api:   api,
		cfg:   cfg,
		sys:   sys,
		cg:    cg,
		aiCli: aiCli,
	}
}

// StartPolling starts listening for Telegram updates in a loop.
func (b *Bot) StartPolling(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)
	log.Printf("[Telegram] Bot @%s successfully started polling. Admin ID: %d", b.api.Self.UserName, b.cfg.TelegramAdminID)

	for {
		select {
		case <-ctx.Done():
			log.Println("[Telegram] Stopping polling listener...")
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.Message == nil {
				continue
			}

			// STRICT SECURITY: Drop any message not from authorized admin
			if update.Message.From.ID != b.cfg.TelegramAdminID {
				log.Printf("[Security Warning] Unauthorized message dropped from user_id=%d (@%s)",
					update.Message.From.ID, update.Message.From.UserName)
				continue
			}

			go b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Commands starting with /
	if strings.HasPrefix(text, "/") {
		parts := strings.Fields(text)
		cmd := strings.ToLower(parts[0])
		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}

		switch cmd {
		case "/start", "/help":
			b.replyHelp(msg.Chat.ID)

		case "/stats", "/status":
			b.replyStats(msg.Chat.ID)

		case "/top":
			b.replyTop(msg.Chat.ID)

		case "/net":
			b.replyNet(msg.Chat.ID)

		case "/ls", "/files":
			targetPath := "."
			if len(args) > 0 {
				targetPath = args[0]
			}
			b.replyListFiles(msg.Chat.ID, targetPath)

		case "/find":
			if len(args) == 0 {
				b.sendText(msg.Chat.ID, "Укажите имя для поиска: <code>/find nginx</code>")
				return
			}
			b.replyFind(msg.Chat.ID, args[0])

		case "/send":
			if len(args) == 0 {
				b.sendText(msg.Chat.ID, "Укажите путь к файлу: <code>/send /var/log/syslog</code>")
				return
			}
			b.replySendFile(msg.Chat.ID, args[0])

		case "/mode":
			modeArg := "auto"
			if len(args) > 0 {
				modeArg = strings.ToLower(args[0])
			}
			b.replyMode(msg.Chat.ID, cgroups.Mode(modeArg))

		case "/vpn_restart":
			b.replyVPNRestart(msg.Chat.ID)

		case "/ai_restart":
			b.replyAIRestart(msg.Chat.ID)

		default:
			b.sendText(msg.Chat.ID, fmt.Sprintf("⚠️ Неизвестная команда <code>%s</code>. Введите /help для справки.", cmd))
		}
		return
	}

	// Plain text: forward to Antigravity AI Brain
	b.handleAIQuery(msg.Chat.ID, text)
}

func (b *Bot) replyHelp(chatID int64) {
	helpText := `<b>🤖 SuperBot — Панель управления VPS</b>

<b>⚡ Системные команды (0 токенов, мгновенно):</b>
• /stats — Метрики CPU, RAM, Swap/ZRAM, Диск, Uptime
• /top — Топ-5 процессов по потреблению памяти
• /net — Сетевой трафик и активные интерфейсы
• /ls &lt;путь&gt; — Просмотр содержимого папки
• /find &lt;маска&gt; — Быстрый поиск файлов на сервере
• /send &lt;путь&gt; — Скачать файл с сервера в Telegram

<b>⚙️ Управление ресурсами (cgroups v2):</b>
• /mode vpn — Турбо-приоритет для VPN (x-ui)
• /mode ai — Турбо-приоритет для Antigravity
• /mode auto — Сбалансированный режим (50/50)
• /vpn_restart — Перезапуск VPN-сервиса (x-ui)
• /ai_restart — Перезапуск AI-сервиса (omniroute)

<b>🧠 Интеллектуальный агент:</b>
<i>Любой обычный текст без слэша (например: «Проверь почту за сегодня») автоматически передается в Antigravity.</i>`

	b.sendText(chatID, helpText)
}

func (b *Bot) replyStats(chatID int64) {
	out, err := b.sys.GetStats()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка сбора метрик: %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyTop(chatID int64) {
	out, err := b.sys.GetTopProcesses()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка /top: %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyNet(chatID int64) {
	out, err := b.sys.GetNetStats()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка /net: %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyListFiles(chatID int64, path string) {
	out, err := b.sys.ListFiles(path)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyFind(chatID int64, pattern string) {
	out, err := b.sys.FindFiles(pattern)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка поиска: %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replySendFile(chatID int64, filePath string) {
	cleanPath, err := b.sys.ValidateFileForSending(filePath)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", err.Error()))
		return
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(cleanPath))
	doc.Caption = fmt.Sprintf("📄 <code>%s</code>", cleanPath)
	doc.ParseMode = "HTML"

	if _, err := b.api.Send(doc); err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка отправки файла: %s", err.Error()))
	}
}

func (b *Bot) replyMode(chatID int64, mode cgroups.Mode) {
	out, err := b.cg.SetMode(mode)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка переключения режима: %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyVPNRestart(chatID int64) {
	out, err := b.cg.RestartVPN()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyAIRestart(chatID int64) {
	out, err := b.cg.RestartAI()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", err.Error()))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) handleAIQuery(chatID int64, prompt string) {
	// Show typing status
	_, _ = b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

	waitMsg, err := b.api.Send(tgbotapi.NewMessage(chatID, "🤔 <i>Запрос передан в Antigravity. Думаю...</i>"))
	waitMsgID := 0
	if err == nil {
		waitMsgID = waitMsg.MessageID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	reply, err := b.aiCli.Ask(ctx, prompt)
	if err != nil {
		errMsg := fmt.Sprintf("⚠️ <b>Antigravity Bridge:</b>\n%s", err.Error())
		if waitMsgID > 0 {
			edit := tgbotapi.NewEditMessageText(chatID, waitMsgID, errMsg)
			edit.ParseMode = "HTML"
			_, _ = b.api.Send(edit)
		} else {
			b.sendText(chatID, errMsg)
		}
		return
	}

	// Update wait message or send new one
	if waitMsgID > 0 && len(reply) < 4000 {
		edit := tgbotapi.NewEditMessageText(chatID, waitMsgID, reply)
		edit.ParseMode = "HTML"
		if _, err := b.api.Send(edit); err != nil {
			// If HTML formatting fails, send as plain text
			edit.ParseMode = ""
			_, _ = b.api.Send(edit)
		}
	} else {
		b.sendText(chatID, reply)
	}
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	if _, err := b.api.Send(msg); err != nil {
		// Fallback to plain text if markup parsing fails
		msg.ParseMode = ""
		_, _ = b.api.Send(msg)
	}
}
