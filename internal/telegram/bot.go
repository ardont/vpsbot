package telegram

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"vpsbot/internal/antigravity"
	"vpsbot/internal/auth"
	"vpsbot/internal/cgroups"
	"vpsbot/internal/config"
	"vpsbot/internal/sysinfo"
)

// Bot manages Telegram updates, user authorization, and command routing.
type Bot struct {
	api    *tgbotapi.BotAPI
	cfg    *config.Config
	auth   *auth.Store
	sys    *sysinfo.SysInfo
	cg     *cgroups.Manager
	aiCli  *antigravity.Client
}

// New creates a new Telegram bot router with user authentication.
func New(
	api *tgbotapi.BotAPI,
	cfg *config.Config,
	authStore *auth.Store,
	sys *sysinfo.SysInfo,
	cg *cgroups.Manager,
	aiCli *antigravity.Client,
) *Bot {
	return &Bot{
		api:   api,
		cfg:   cfg,
		auth:  authStore,
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
	log.Printf("[Telegram] Bot @%s started polling. Master Admin ID: %d", b.api.Self.UserName, b.cfg.TelegramAdminID)

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

			go b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	userID := msg.From.ID
	userName := msg.From.UserName

	// Check if user is registered/authorized
	if !b.auth.IsAuthorized(userID) {
		// Allow /register <invite_code>
		if strings.HasPrefix(strings.ToLower(text), "/register") {
			parts := strings.Fields(text)
			if len(parts) < 2 {
				b.sendText(msg.Chat.ID, "Укажите инвайт-код: <code>/register ВАШ_КОД</code>")
				return
			}
			code := strings.ToLower(parts[1])
			if err := b.auth.RedeemInvite(code, userID, userName); err != nil {
				b.sendText(msg.Chat.ID, fmt.Sprintf("❌ <b>Ошибка регистрации:</b> %s", err.Error()))
				return
			}

			b.sendText(msg.Chat.ID, "🎉 <b>Доступ успешно предоставлен!</b>\nДобро пожаловать в SuperBot. Введите /help для просмотра команд.")

			// Notify Master Admin
			adminNotice := fmt.Sprintf(
				"✅ <b>Новый пользователь зарегистрирован по инвайт-коду:</b>\n• User: @%s\n• ID: <code>%d</code>",
				userName, userID,
			)
			b.sendText(b.cfg.TelegramAdminID, adminNotice)
			return
		}

		// Deny access and display instructions
		accessDeniedMsg := fmt.Sprintf(
			"🔒 <b>Доступ ограничен</b>\n\n"+
				"Бот работает в приватном режиме для авторизованных пользователей сервера.\n"+
				"Ваш Telegram ID: <code>%d</code>\n\n"+
				"<b>Как получить доступ:</b>\n"+
				"1. Передайте ваш ID администратору сервера для добавления.\n"+
				"2. Или активируйте полученный инвайт-код командой:\n"+
				"<code>/register ВАШ_КОД</code>",
			userID,
		)
		b.sendText(msg.Chat.ID, accessDeniedMsg)

		// Alert Master Admin about access request
		fullName := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
		adminAlert := fmt.Sprintf(
			"🔔 <b>Запрос доступа к SuperBot:</b>\n"+
				"• Пользователь: @%s (%s)\n"+
				"• ID: <code>%d</code>\n\n"+
				"Чтобы выдать доступ в 1 клик, отправьте:\n"+
				"<code>/adduser %d %s</code>",
			userName, fullName, userID, userID, userName,
		)
		b.sendText(b.cfg.TelegramAdminID, adminAlert)
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
			b.replyHelp(msg.Chat.ID, userID)

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

		// === ADMIN-ONLY COMMANDS ===
		case "/invite":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			hours := 24
			if len(args) > 0 {
				if h, err := strconv.Atoi(args[0]); err == nil && h > 0 {
					hours = h
				}
			}
			b.replyCreateInvite(msg.Chat.ID, userID, hours)

		case "/adduser":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			if len(args) == 0 {
				b.sendText(msg.Chat.ID, "Использование: <code>/adduser &lt;id&gt; [username]</code>")
				return
			}
			newID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				b.sendText(msg.Chat.ID, "❌ ID пользователя должен быть числом.")
				return
			}
			uName := ""
			if len(args) > 1 {
				uName = strings.TrimPrefix(args[1], "@")
			}
			if err := b.auth.AddUser(newID, uName, auth.RoleUser); err != nil {
				b.sendText(msg.Chat.ID, fmt.Sprintf("❌ Ошибка: %s", err.Error()))
				return
			}
			b.sendText(msg.Chat.ID, fmt.Sprintf("✅ Пользователь <code>%d</code> (@%s) успешно добавлен в белый список!", newID, uName))
			// Notify added user
			b.sendText(newID, "🎉 <b>Вам предоставлен доступ к SuperBot!</b> Введите /help для справки.")

		case "/deluser":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			if len(args) == 0 {
				b.sendText(msg.Chat.ID, "Использование: <code>/deluser &lt;id&gt;</code>")
				return
			}
			targetID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				b.sendText(msg.Chat.ID, "❌ ID пользователя должен быть числом.")
				return
			}
			if err := b.auth.RemoveUser(targetID); err != nil {
				b.sendText(msg.Chat.ID, fmt.Sprintf("❌ Ошибка: %s", err.Error()))
				return
			}
			b.sendText(msg.Chat.ID, fmt.Sprintf("🗑️ Пользователь <code>%d</code> удален из системы.", targetID))

		case "/users":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			b.replyListUsers(msg.Chat.ID)

		case "/mode":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			modeArg := "auto"
			if len(args) > 0 {
				modeArg = strings.ToLower(args[0])
			}
			b.replyMode(msg.Chat.ID, cgroups.Mode(modeArg))

		case "/vpn_restart":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			b.replyVPNRestart(msg.Chat.ID)

		case "/ai_restart":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			b.replyAIRestart(msg.Chat.ID)

		default:
			b.sendText(msg.Chat.ID, fmt.Sprintf("⚠️ Неизвестная команда <code>%s</code>. Введите /help для справки.", cmd))
		}
		return
	}

	// Plain text: forward to Antigravity AI Brain
	b.handleAIQuery(msg.Chat.ID, text)
}

func (b *Bot) checkAdmin(chatID, userID int64) bool {
	if !b.auth.IsAdmin(userID) {
		b.sendText(chatID, "⛔ Эта команда доступна только администраторам сервера.")
		return false
	}
	return true
}

func (b *Bot) replyHelp(chatID, userID int64) {
	isAdmin := b.auth.IsAdmin(userID)

	var sb strings.Builder
	sb.WriteString("<b>🤖 SuperBot — Панель управления VPS</b>\n\n")
	sb.WriteString("<b>⚡ Системные команды:</b>\n")
	sb.WriteString("• /stats — Метрики CPU, RAM, ZRAM/Swap, Диск, Uptime\n")
	sb.WriteString("• /top — Топ-5 процессов по потреблению памяти\n")
	sb.WriteString("• /net — Сетевой трафик (TX/RX) и интерфейсы\n")
	sb.WriteString("• /ls &lt;путь&gt; — Просмотр файлов в папке\n")
	sb.WriteString("• /find &lt;маска&gt; — Быстрый поиск файлов\n")
	sb.WriteString("• /send &lt;путь&gt; — Скачать файл в Telegram\n\n")

	if isAdmin {
		sb.WriteString("<b>👑 Управление пользователями и доступом:</b>\n")
		sb.WriteString("• /invite [часы] — Создать одноразовый инвайт-код\n")
		sb.WriteString("• /adduser &lt;id&gt; [username] — Добавить ID в белый список\n")
		sb.WriteString("• /deluser &lt;id&gt; — Отозвать доступ у пользователя\n")
		sb.WriteString("• /users — Список всех авторизованных пользователей\n\n")

		sb.WriteString("<b>⚙️ Управление ресурсами ядра (cgroups v2):</b>\n")
		sb.WriteString("• /mode vpn — Турбо-приоритет для VPN (x-ui)\n")
		sb.WriteString("• /mode ai — Турбо-приоритет для Antigravity\n")
		sb.WriteString("• /mode auto — Сбалансированный режим (50/50)\n")
		sb.WriteString("• /vpn_restart — Перезапуск x-ui.service\n")
		sb.WriteString("• /ai_restart — Перезапуск omniroute.service\n\n")
	}

	sb.WriteString("<b>🧠 Интеллектуальный агент:</b>\n")
	sb.WriteString("<i>Отправьте любой обычный текст без слэша (например: «Проверь почту за сегодня»), чтобы передать задачу в Antigravity.</i>")

	b.sendText(chatID, sb.String())
}

func (b *Bot) replyCreateInvite(chatID, userID int64, hours int) {
	ttl := time.Duration(hours) * time.Hour
	code, err := b.auth.GenerateInvite(userID, ttl)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка генерации инвайт-кода: %s", err.Error()))
		return
	}

	text := fmt.Sprintf(
		"🎟️ <b>Создан одноразовый инвайт-код:</b>\n\n"+
			"<code>%s</code>\n\n"+
			"⏱ <i>Действителен: %d ч.</i>\n\n"+
			"Передайте этот код новому пользователю. Ему достаточно отправить боту:\n"+
			"<code>/register %s</code>",
		code, hours, code,
	)
	b.sendText(chatID, text)
}

func (b *Bot) replyListUsers(chatID int64) {
	users := b.auth.ListUsers()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👥 <b>Авторизованные пользователи (%d):</b>\n\n", len(users)))

	for _, u := range users {
		roleBadge := "👤 user"
		if u.Role == auth.RoleAdmin {
			roleBadge = "👑 admin"
		}
		uName := u.Username
		if uName == "" {
			uName = "без @username"
		} else {
			uName = "@" + uName
		}
		sb.WriteString(fmt.Sprintf("• <code>%d</code> | %s (%s)\n", u.ID, uName, roleBadge))
	}

	b.sendText(chatID, sb.String())
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

	if waitMsgID > 0 && len(reply) < 4000 {
		edit := tgbotapi.NewEditMessageText(chatID, waitMsgID, reply)
		edit.ParseMode = "HTML"
		if _, err := b.api.Send(edit); err != nil {
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
		msg.ParseMode = ""
		_, _ = b.api.Send(msg)
	}
}
