package telegram

import (
	"context"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"vpsbot/internal/antigravity"
	"vpsbot/internal/auth"
	"vpsbot/internal/cgroups"
	"vpsbot/internal/config"
	"vpsbot/internal/sysinfo"
)

// Bot manages Telegram updates, user authorization, inline menus, and AI bridge.
type Bot struct {
	api      *tgbotapi.BotAPI
	cfg      *config.Config
	auth     *auth.Store
	sys      *sysinfo.SysInfo
	cg       *cgroups.Manager
	aiCli    *antigravity.Client
	sessions map[int64]string
	sessMu   sync.RWMutex
}

// New creates a new Telegram bot router.
func New(
	api *tgbotapi.BotAPI,
	cfg *config.Config,
	authStore *auth.Store,
	sys *sysinfo.SysInfo,
	cg *cgroups.Manager,
	aiCli *antigravity.Client,
) *Bot {
	return &Bot{
		api:      api,
		cfg:      cfg,
		auth:     authStore,
		sys:      sys,
		cg:       cg,
		aiCli:    aiCli,
		sessions: make(map[int64]string),
	}
}

func (b *Bot) getSession(userID int64) string {
	b.sessMu.RLock()
	defer b.sessMu.RUnlock()
	return b.sessions[userID]
}

func (b *Bot) setSession(userID int64, convID string) {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	b.sessions[userID] = convID
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

			if update.CallbackQuery != nil {
				go b.handleCallback(update.CallbackQuery)
				continue
			}

			if update.Message != nil {
				go b.handleMessage(update.Message)
			}
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	userID := msg.From.ID
	userName := msg.From.UserName

	// Check user authorization
	if !b.auth.IsAuthorized(userID) {
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

			b.sendMainMenu(msg.Chat.ID, "🎉 <b>Доступ успешно предоставлен!</b>\nДобро пожаловать в SuperBot.")

			adminNotice := fmt.Sprintf(
				"✅ <b>Новый пользователь зарегистрирован по инвайту:</b>\n• User: @%s\n• ID: <code>%d</code>",
				userName, userID,
			)
			b.sendText(b.cfg.TelegramAdminID, adminNotice)
			return
		}

		accessDeniedMsg := fmt.Sprintf(
			"🔒 <b>Доступ ограничен</b>\n\n"+
				"Бот работает в приватном режиме для авторизованных пользователей сервера.\n"+
				"Ваш Telegram ID: <code>%d</code>\n\n"+
				"<b>Как получить доступ:</b>\n"+
				"1. Передайте ваш ID администратору сервера.\n"+
				"2. Или активируйте инвайт-код командой:\n"+
				"<code>/register ВАШ_КОД</code>",
			userID,
		)
		b.sendText(msg.Chat.ID, accessDeniedMsg)

		fullName := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
		adminAlert := fmt.Sprintf(
			"🔔 <b>Запрос доступа:</b> @%s (%s)\nID: <code>%d</code>\nДля добавления: <code>/adduser %d %s</code>",
			userName, fullName, userID, userID, userName,
		)
		b.sendText(b.cfg.TelegramAdminID, adminAlert)
		return
	}

	// Remove any old lingering reply keyboards
	if strings.HasPrefix(text, "/") {
		parts := strings.Fields(text)
		cmd := strings.ToLower(parts[0])
		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}

		switch cmd {
		case "/start", "/menu":
			b.sendMainMenu(msg.Chat.ID, "👋 <b>Панель управления VPS & AI</b>\nВыберите действие в меню ниже или напишите любой вопрос:")

		case "/help":
			b.replyHelp(msg.Chat.ID, userID)

		case "/stats", "/status":
			b.replyStats(msg.Chat.ID)

		case "/top":
			b.replyTop(msg.Chat.ID)

		case "/net":
			b.replyNet(msg.Chat.ID)

		case "/mail", "/inbox":
			b.replyMail(msg.Chat.ID)

		case "/drop_caches", "/clean":
			b.replyDropCaches(msg.Chat.ID)

		case "/new", "/reset":
			b.setSession(userID, "")
			b.sendText(msg.Chat.ID, "🔄 <b>Контекст очищен!</b>\nНачат новый диалог с Antigravity. Задайте любой вопрос.")

		case "/ls", "/files":
			targetPath := "/root"
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
				b.sendText(msg.Chat.ID, "Укажите путь к файлу: <code>/send /root/test.txt</code>")
				return
			}
			b.replySendFile(msg.Chat.ID, args[0])

		case "/mode":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			if len(args) == 0 {
				b.sendPriorityMenu(msg.Chat.ID)
				return
			}
			b.replyMode(msg.Chat.ID, cgroups.Mode(strings.ToLower(args[0])))

		case "/vpn_restart":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyVPNRestart(msg.Chat.ID)
			}

		case "/ai_restart":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyAIRestart(msg.Chat.ID)
			}

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

		case "/users":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyListUsers(msg.Chat.ID)
			}

		case "/adduser":
			if !b.checkAdmin(msg.Chat.ID, userID) {
				return
			}
			if len(args) == 0 {
				b.sendText(msg.Chat.ID, "Использование: <code>/adduser &lt;id&gt; [username]</code>")
				return
			}
			targetID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				b.sendText(msg.Chat.ID, "❌ ID пользователя должен быть числом.")
				return
			}
			tName := ""
			if len(args) > 1 {
				tName = args[1]
			}
			if err := b.auth.AddUser(targetID, tName, auth.RoleUser); err != nil {
				b.sendText(msg.Chat.ID, fmt.Sprintf("❌ Ошибка: %s", err.Error()))
				return
			}
			b.sendText(msg.Chat.ID, fmt.Sprintf("✅ Пользователь <code>%d</code> (@%s) успешно добавлен.", targetID, tName))

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
			b.sendText(msg.Chat.ID, fmt.Sprintf("🗑 Пользователь <code>%d</code> удален из системы.", targetID))

		default:
			b.sendText(msg.Chat.ID, "❓ Неизвестная команда. Напишите /help или выберите пункт меню.")
		}
		return
	}

	// Normal text query -> send to Antigravity AI Bridge
	b.handleAIQuery(msg.Chat.ID, userID, text)
}

func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	// Acknowledge callback immediately
	ack := tgbotapi.NewCallback(cb.ID, "")
	_, _ = b.api.Request(ack)

	data := cb.Data
	chatID := cb.Message.Chat.ID
	userID := cb.From.ID
	msgID := cb.Message.MessageID

	if !b.auth.IsAuthorized(userID) {
		b.sendText(chatID, "🔒 Доступ запрещен.")
		return
	}

	// Handle interactive AI options (ask_question)
	if strings.HasPrefix(data, "ai_opt:") {
		selectedOpt := strings.TrimPrefix(data, "ai_opt:")
		b.handleAIQuery(chatID, userID, selectedOpt)
		return
	}

	switch data {
	case "menu_main":
		b.editOrSendMenu(chatID, msgID, "👋 <b>Главное меню SuperBot</b>\nВыберите действие или отправьте задачу текстом:")

	case "menu_all":
		b.showAllFunctionsMenu(chatID, msgID)

	case "menu_server":
		b.showServerMenu(chatID, msgID)

	case "menu_net":
		b.showNetworkMenu(chatID, msgID)

	case "menu_files":
		b.showFilesMenu(chatID, msgID)

	case "menu_prio":
		b.showPriorityMenu(chatID, msgID)

	case "menu_ai":
		b.showAIMenu(chatID, msgID, userID)

	case "menu_users":
		if b.checkAdmin(chatID, userID) {
			b.showUsersMenu(chatID, msgID)
		}

	case "act_mail":
		b.replyMail(chatID)

	case "act_status":
		b.replyQuickStatus(chatID)

	case "act_stats_full":
		b.replyStats(chatID)

	case "act_top":
		b.replyTop(chatID)

	case "act_drop_caches":
		b.replyDropCaches(chatID)

	case "act_net_info":
		b.replyNetInfo(chatID)

	case "act_net_stats":
		b.replyNet(chatID)

	case "act_ls_root":
		b.replyListFiles(chatID, "/root")

	case "act_mode_ai":
		if b.checkAdmin(chatID, userID) {
			b.replyMode(chatID, cgroups.ModeAI)
		}

	case "act_mode_vpn":
		if b.checkAdmin(chatID, userID) {
			b.replyMode(chatID, cgroups.ModeVPN)
		}

	case "act_mode_auto":
		if b.checkAdmin(chatID, userID) {
			b.replyMode(chatID, cgroups.ModeBalanced)
		}

	case "act_restart_vpn":
		if b.checkAdmin(chatID, userID) {
			b.replyVPNRestart(chatID)
		}

	case "act_restart_ai":
		if b.checkAdmin(chatID, userID) {
			b.replyAIRestart(chatID)
		}

	case "act_new_ai":
		b.setSession(userID, "")
		b.sendText(chatID, "🔄 <b>Контекст очищен!</b>\nНачат новый диалог с Antigravity. Задайте любой вопрос.")

	case "act_ai_info":
		sess := b.getSession(userID)
		if sess == "" {
			sess = "<i>(новая сессия еще не создана)</i>"
		} else {
			sess = "<code>" + sess + "</code>"
		}
		b.sendText(chatID, fmt.Sprintf("🧠 <b>Активная сессия Antigravity:</b>\n%s\n\nМодель: <code>gemini-3.8-flash-high</code>\nМост: <code>agy-pilot (:43430)</code>", sess))

	case "act_create_invite":
		if b.checkAdmin(chatID, userID) {
			b.replyCreateInvite(chatID, userID, 24)
		}

	case "act_list_users":
		if b.checkAdmin(chatID, userID) {
			b.replyListUsers(chatID)
		}
	}
}

// ==============================================================================
// Клавиатуры и Навигация
// ==============================================================================

func (b *Bot) getMainMenuMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📬 Почта (Fast)", "act_mail"),
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус (Fast)", "act_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚡ Приоритеты", "menu_prio"),
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Все функции", "menu_all"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Новый диалог ИИ", "act_new_ai"),
		),
	)
}

func (b *Bot) sendMainMenu(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = b.getMainMenuMarkup()
	_, _ = b.api.Send(msg)
}

func (b *Bot) editOrSendMenu(chatID int64, msgID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	edit.ParseMode = "HTML"
	markup := b.getMainMenuMarkup()
	edit.ReplyMarkup = &markup
	if _, err := b.api.Send(edit); err != nil {
		b.sendMainMenu(chatID, text)
	}
}

func (b *Bot) showAllFunctionsMenu(chatID int64, msgID int) {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🖥 Сервер & Память", "menu_server"),
			tgbotapi.NewInlineKeyboardButtonData("🌐 Сеть & IP", "menu_net"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📁 Файлы", "menu_files"),
			tgbotapi.NewInlineKeyboardButtonData("🧠 ИИ & Сессии", "menu_ai"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👑 Пользователи", "menu_users"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Главное меню", "menu_main"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "⚙️ <b>Все функции сервера:</b>\nВыберите категорию для управления:")
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	if _, err := b.api.Send(edit); err != nil {
		msg := tgbotapi.NewMessage(chatID, "⚙️ <b>Все функции сервера:</b>\nВыберите категорию для управления:")
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = markup
		_, _ = b.api.Send(msg)
	}
}

func (b *Bot) showServerMenu(chatID int64, msgID int) {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Полная сводка", "act_stats_full"),
			tgbotapi.NewInlineKeyboardButtonData("🧹 Очистить RAM", "act_drop_caches"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔝 Топ процессов", "act_top"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Рестарт OmniRoute", "act_restart_ai"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к категориям", "menu_all"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "🖥 <b>Управление сервером и памятью:</b>\nВсе функции выполняются мгновенно за 0.1 сек:")
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	_, _ = b.api.Send(edit)
}

func (b *Bot) showNetworkMenu(chatID int64, msgID int) {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🌍 Внешний IP & TS", "act_net_info"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Рестарт VPN", "act_restart_vpn"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Сетевой трафик", "act_net_stats"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к категориям", "menu_all"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "🌐 <b>Сеть, Tailscale & VPN:</b>\nВыберите действие:")
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	_, _ = b.api.Send(edit)
}

func (b *Bot) showFilesMenu(chatID int64, msgID int) {
	fileBrowserURL := "https://vm-576151.tailb9b76e.ts.net:10443"
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📂 Список /root", "act_ls_root"),
			tgbotapi.NewInlineKeyboardButtonURL("🔗 FileBrowser Web", fileBrowserURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к категориям", "menu_all"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "📁 <b>Файловый менеджер:</b>\nПросматривайте файлы или откройте веб-интерфейс FileBrowser:")
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	_, _ = b.api.Send(edit)
}

func (b *Bot) showPriorityMenu(chatID int64, msgID int) {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 Турбо ИИ (OmniRoute)", "act_mode_ai"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛡 Турбо VPN (x-ui)", "act_mode_vpn"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚖️ Сбалансированный (50/50)", "act_mode_auto"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Главное меню", "menu_main"),
		),
	)

	text := "⚡ <b>Приоритеты cgroups v2:</b>\nПерераспределение квантов CPU и лимитов памяти между задачами:"
	if msgID > 0 {
		edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
		edit.ParseMode = "HTML"
		edit.ReplyMarkup = &markup
		_, _ = b.api.Send(edit)
	} else {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = markup
		_, _ = b.api.Send(msg)
	}
}

func (b *Bot) sendPriorityMenu(chatID int64) {
	b.showPriorityMenu(chatID, 0)
}

func (b *Bot) showAIMenu(chatID int64, msgID int, userID int64) {
	sess := b.getSession(userID)
	statusText := "Сессия: <i>новая</i>"
	if sess != "" {
		statusText = fmt.Sprintf("Сессия: <code>%s</code>", sess[:8]+"...")
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Начать новый диалог", "act_new_ai"),
			tgbotapi.NewInlineKeyboardButtonData("ℹ️ О сессии", "act_ai_info"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к категориям", "menu_all"),
		),
	)

	text := fmt.Sprintf("🧠 <b>Интеллектуальный агент Antigravity:</b>\n%s\n\nМост: <code>agy-pilot (:43430)</code>\nДля общения просто напишите вопрос в чат.", statusText)
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	_, _ = b.api.Send(edit)
}

func (b *Bot) showUsersMenu(chatID int64, msgID int) {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎟 Создать инвайт (24ч)", "act_create_invite"),
			tgbotapi.NewInlineKeyboardButtonData("👥 Список пользователей", "act_list_users"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к категориям", "menu_all"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "👑 <b>Управление доступом и инвайтами:</b>")
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	_, _ = b.api.Send(edit)
}

// ==============================================================================
// Мгновенные команды (Zero-Token Fast Handlers)
// ==============================================================================

func (b *Bot) replyQuickStatus(chatID int64) {
	out, err := b.sys.GetQuickStatus()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка статуса: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendTextWithMenu(chatID, out)
}

func (b *Bot) replyDropCaches(chatID int64) {
	out, err := b.sys.DropCaches()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка очистки кэша: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendTextWithMenu(chatID, out)
}

func (b *Bot) replyNetInfo(chatID int64) {
	out, err := b.sys.GetNetworkAndIP()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка сети: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendTextWithMenu(chatID, out)
}

func (b *Bot) replyMail(chatID int64) {
	_, _ = b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
	waitMsg, _ := b.api.Send(tgbotapi.NewMessage(chatID, "📬 <i>Проверяю почтовые ящики (Gmail & Mail.ru)...</i>"))

	out, err := b.sys.CheckMail()
	if waitMsg.MessageID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, waitMsg.MessageID))
	}

	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ <b>Ошибка проверки почты:</b>\n%s", html.EscapeString(err.Error())))
		return
	}

	b.sendTextWithMenu(chatID, out)
}

func (b *Bot) replyStats(chatID int64) {
	out, err := b.sys.GetStats()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка сбора метрик: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyTop(chatID int64) {
	out, err := b.sys.GetTopProcesses()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка /top: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyNet(chatID int64) {
	out, err := b.sys.GetNetStats()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка /net: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyListFiles(chatID int64, path string) {
	out, err := b.sys.ListFiles(path)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyFind(chatID int64, pattern string) {
	out, err := b.sys.FindFiles(pattern)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка поиска: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replySendFile(chatID int64, filePath string) {
	cleanPath, err := b.sys.ValidateFileForSending(filePath)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", html.EscapeString(err.Error())))
		return
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(cleanPath))
	doc.Caption = fmt.Sprintf("📄 <code>%s</code>", html.EscapeString(cleanPath))
	doc.ParseMode = "HTML"

	if _, err := b.api.Send(doc); err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка отправки файла: %s", html.EscapeString(err.Error())))
	}
}

func (b *Bot) replyMode(chatID int64, mode cgroups.Mode) {
	out, err := b.cg.SetMode(mode)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка переключения режима: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyVPNRestart(chatID int64) {
	out, err := b.cg.RestartVPN()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyAIRestart(chatID int64) {
	out, err := b.cg.RestartAI()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyCreateInvite(chatID, userID int64, hours int) {
	ttl := time.Duration(hours) * time.Hour
	code, err := b.auth.GenerateInvite(userID, ttl)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка генерации инвайт-кода: %s", html.EscapeString(err.Error())))
		return
	}

	text := fmt.Sprintf(
		"🎟️ <b>Одноразовый инвайт-код:</b>\n\n"+
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
		sb.WriteString(fmt.Sprintf("• <code>%d</code> | %s (%s)\n", u.ID, html.EscapeString(uName), roleBadge))
	}

	b.sendText(chatID, sb.String())
}

func (b *Bot) replyHelp(chatID, userID int64) {
	isAdmin := b.auth.IsAdmin(userID)

	var sb strings.Builder
	sb.WriteString("<b>🤖 SuperBot — Панель управления VPS</b>\n\n")
	sb.WriteString("<b>⚡ Мгновенные команды (0.1 сек, Zero-Token):</b>\n")
	sb.WriteString("• /stats — Метрики CPU, RAM, ZRAM/Swap, Диск, Uptime\n")
	sb.WriteString("• /mail — Проверка почты (Gmail & Mail.ru)\n")
	sb.WriteString("• /drop_caches — Очистка неактивного кэша RAM\n")
	sb.WriteString("• /net — Сетевая активность и интерфейсы\n")
	sb.WriteString("• /top — Топ процессов по памяти\n")
	sb.WriteString("• /ls &lt;путь&gt; — Просмотр файлов в папке\n")
	sb.WriteString("• /find &lt;маска&gt; — Поиск файлов\n")
	sb.WriteString("• /send &lt;путь&gt; — Скачать файл в Telegram\n")
	sb.WriteString("• /new — Сбросить контекст диалога с ИИ\n\n")

	if isAdmin {
		sb.WriteString("<b>👑 Команды администратора:</b>\n")
		sb.WriteString("• /invite [часы] — Создать одноразовый инвайт-код\n")
		sb.WriteString("• /adduser &lt;id&gt; [username] — Добавить ID в белый список\n")
		sb.WriteString("• /deluser &lt;id&gt; — Отозвать доступ у пользователя\n")
		sb.WriteString("• /users — Список всех пользователей\n")
		sb.WriteString("• /mode [ai|vpn|auto] — Переключение приоритетов cgroups\n\n")
	}

	sb.WriteString("<b>🧠 Интеллектуальный агент:</b>\n")
	sb.WriteString("<i>Отправьте любой текст без слэша, чтобы передать сложную задачу ядру Antigravity.</i>")

	b.sendTextWithMenu(chatID, sb.String())
}

func (b *Bot) checkAdmin(chatID, userID int64) bool {
	if !b.auth.IsAdmin(userID) {
		b.sendText(chatID, "⛔ Эта команда доступна только администраторам сервера.")
		return false
	}
	return true
}

// ==============================================================================
// Antigravity AI Bridge & Interactive Options
// ==============================================================================

func (b *Bot) handleAIQuery(chatID, userID int64, prompt string) {
	_, _ = b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

	waitMsg, err := b.api.Send(tgbotapi.NewMessage(chatID, "🤔 <i>Передаю запрос в Antigravity. Думаю...</i>"))
	waitMsgID := 0
	if err == nil {
		waitMsgID = waitMsg.MessageID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	activeConvID := b.getSession(userID)
	resp, err := b.aiCli.Ask(ctx, prompt, activeConvID, false)
	if err != nil {
		errMsg := fmt.Sprintf("⚠️ <b>Antigravity Bridge:</b>\n%s", html.EscapeString(err.Error()))
		if waitMsgID > 0 {
			edit := tgbotapi.NewEditMessageText(chatID, waitMsgID, errMsg)
			edit.ParseMode = "HTML"
			_, _ = b.api.Send(edit)
		} else {
			b.sendText(chatID, errMsg)
		}
		return
	}

	if resp.ConversationID != "" {
		b.setSession(userID, resp.ConversationID)
	}

	reply := resp.Text
	if reply == "" && resp.Question != "" {
		reply = "❓ <b>" + html.EscapeString(resp.Question) + "</b>"
	}

	// Prepare inline buttons if ask_question options were returned
	var optMarkup *tgbotapi.InlineKeyboardMarkup
	if len(resp.Options) > 0 {
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, opt := range resp.Options {
			cleanOpt := strings.TrimPrefix(opt, "(Recommended) ")
			btnText := opt
			if len(btnText) > 36 {
				btnText = btnText[:34] + "…"
			}
			btnData := "ai_opt:" + cleanOpt
			if len(btnData) > 64 {
				btnData = btnData[:64]
			}
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(btnText, btnData),
			))
		}
		markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
		optMarkup = &markup
	}

	// Remove typing message
	if waitMsgID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, waitMsgID))
	}

	// Deliver reply safely (splitting chunks if necessary)
	b.sendHTMLChunked(chatID, reply, optMarkup)
}

// ==============================================================================
// Утилиты отправки сообщений и форматирования HTML
// ==============================================================================

func (b *Bot) sendText(chatID int64, text string) {
	b.sendHTMLChunked(chatID, text, nil)
}

func (b *Bot) sendTextWithMenu(chatID int64, text string) {
	markup := b.getMainMenuMarkup()
	b.sendHTMLChunked(chatID, text, &markup)
}

func (b *Bot) sendHTMLChunked(chatID int64, text string, markup *tgbotapi.InlineKeyboardMarkup) {
	const maxChunk = 3900

	if len(text) <= maxChunk {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "HTML"
		if markup != nil {
			msg.ReplyMarkup = markup
		}
		if _, err := b.api.Send(msg); err != nil {
			// Fallback to plain text if HTML parsing failed
			msg.ParseMode = ""
			_, _ = b.api.Send(msg)
		}
		return
	}

	// Split by paragraphs
	lines := strings.Split(text, "\n")
	var currentChunk strings.Builder

	for _, line := range lines {
		if currentChunk.Len()+len(line)+1 > maxChunk {
			chunkText := currentChunk.String()
			msg := tgbotapi.NewMessage(chatID, chunkText)
			msg.ParseMode = "HTML"
			if _, err := b.api.Send(msg); err != nil {
				msg.ParseMode = ""
				_, _ = b.api.Send(msg)
			}
			currentChunk.Reset()
		}
		currentChunk.WriteString(line + "\n")
	}

	if currentChunk.Len() > 0 {
		msg := tgbotapi.NewMessage(chatID, currentChunk.String())
		msg.ParseMode = "HTML"
		if markup != nil {
			msg.ReplyMarkup = markup
		}
		if _, err := b.api.Send(msg); err != nil {
			msg.ParseMode = ""
			_, _ = b.api.Send(msg)
		}
	}
}
