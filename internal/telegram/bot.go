package telegram

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"vpsbot/internal/antigravity"
	"vpsbot/internal/auth"
	"vpsbot/internal/cgroups"
	"vpsbot/internal/config"
	"vpsbot/internal/formatter"
	"vpsbot/internal/sysinfo"
)

// Bot manages Telegram updates, user authorization, inline menus, and AI bridge.
type Bot struct {
	api        *tgbotapi.BotAPI
	cfg        *config.Config
	auth       *auth.Store
	sys        *sysinfo.SysInfo
	cg         *cgroups.Manager
	aiCli      *antigravity.Client
	sessions   map[int64]string
	sessMu     sync.RWMutex
	lastKbSent sync.Map // map[int64]time.Time — throttle persistent keyboard resend
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
	userID := msg.From.ID
	userName := msg.From.UserName
	text := strings.TrimSpace(msg.Text)

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

	// 1. Handle incoming Photos
	if msg.Photo != nil && len(msg.Photo) > 0 {
		b.handleIncomingPhoto(msg)
		return
	}

	// 2. Handle incoming Documents
	if msg.Document != nil {
		b.handleIncomingDocument(msg)
		return
	}

	// 3. Handle Persistent Bottom Keyboard Buttons
	switch strings.TrimSpace(text) {
	case "📬 Почта":
		b.replyMail(msg.Chat.ID)
		return
	case "📊 Статус", "📊 Сервер":
		b.replyQuickStatus(msg.Chat.ID)
		return
	case "⚡ Приоритеты", "⚡ Турбо Режим":
		b.sendPriorityMenu(msg.Chat.ID)
		return
	case "🧹 Очистить RAM", "🧹 Очистить кэш", "🧹 Очистка":
		b.replyDropCaches(msg.Chat.ID)
		return
	case "🌐 Сеть & VPN", "🌐 Сеть & TS", "🌍 Сеть & TS", "🌐 Сеть":
		b.replyNetInfo(msg.Chat.ID)
		return
	case "📦 Бэкап", "📦 Бэкапы":
		b.replyBackupInfo(msg.Chat.ID)
		return
	case "📋 Логи", "📋 Логи сервисов":
		b.replyLogsMenu(msg.Chat.ID)
		return
	case "🔄 Новый диалог", "🔄 Новый диалог ИИ", "🔄 Сброс контекста":
		b.setSession(userID, "")
		b.sendText(msg.Chat.ID, "🔄 <b>Контекст очищен!</b>\nНачат новый диалог с Antigravity. Задайте любой вопрос или отправьте файл.")
		return
	case "⚙️ Меню", "⚙️ Все функции":
		b.sendMainMenu(msg.Chat.ID, "⚙️ <b>Главное меню SuperBot:</b>\nВыберите действие или категорию:")
		return
	}

	// 4. Handle Slash Commands
	if strings.HasPrefix(text, "/") {
		parts := strings.Fields(text)
		cmd := strings.ToLower(parts[0])
		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}

		switch cmd {
		case "/start", "/menu":
			b.sendPersistentKeyboard(msg.Chat.ID, "👋 <b>Добро пожаловать в SuperBot!</b>\nКнопки быстрого доступа закреплены внизу экрана 👇")
			b.sendMainMenu(msg.Chat.ID, "⚙️ <b>Интерактивные разделы:</b>")

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

		case "/backup":
			b.replyBackupInfo(msg.Chat.ID)

		case "/make_backup":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyCreateBackup(msg.Chat.ID)
			}

		case "/errors":
			b.replyRecentErrors(msg.Chat.ID)

		case "/turbo_ai":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyMode(msg.Chat.ID, cgroups.ModeAI)
			}

		case "/turbo_vpn":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyMode(msg.Chat.ID, cgroups.ModeVPN)
			}

		case "/balance":
			if b.checkAdmin(msg.Chat.ID, userID) {
				b.replyMode(msg.Chat.ID, cgroups.ModeBalanced)
			}

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
				b.sendText(msg.Chat.ID, "Укажите путь к файлу или папке: <code>/send /root/test.txt</code>")
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

	// 4. Normal text query -> send to Antigravity AI Bridge
	b.handleAIQuery(msg.Chat.ID, userID, text)
}

// ==============================================================================
// Обработка Фото и Документов (Мультимодальность)
// ==============================================================================

func (b *Bot) handleIncomingPhoto(msg *tgbotapi.Message) {
	photos := msg.Photo
	largest := photos[len(photos)-1]

	_ = b.sendTyping(msg.Chat.ID)
	statusMsg, _ := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "📸 <i>Загружаю фото и провожу OCR анализ...</i>"))

	timestamp := time.Now().Unix()
	localPath := fmt.Sprintf("/tmp/user_upload_%d.jpg", timestamp)

	if err := b.downloadTelegramFile(largest.FileID, localPath); err != nil {
		if statusMsg.MessageID > 0 {
			_, _ = b.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, statusMsg.MessageID))
		}
		b.sendText(msg.Chat.ID, fmt.Sprintf("❌ Ошибка сохранения фото: %s", html.EscapeString(err.Error())))
		return
	}

	// Run OCR via Tesseract
	ocrBytes, _ := exec.Command("tesseract", localPath, "stdout", "-l", "rus+eng", "--oem", "1").Output()
	ocrText := strings.TrimSpace(string(ocrBytes))

	caption := strings.TrimSpace(msg.Caption)
	if caption == "" {
		caption = "Что видно на этом изображении? Проанализируй текст и детали."
	}

	var prompt strings.Builder
	prompt.WriteString(fmt.Sprintf("Пользователь прислал изображение (сохранено на сервере в '%s').\n", localPath))
	if ocrText != "" {
		prompt.WriteString(fmt.Sprintf("Распознанный текст на изображении (Tesseract OCR):\n\"\"\"\n%s\n\"\"\"\n\n", ocrText))
	}
	prompt.WriteString(fmt.Sprintf("Вопрос / задание пользователя: %s", caption))

	if statusMsg.MessageID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, statusMsg.MessageID))
	}

	b.handleAIQuery(msg.Chat.ID, msg.From.ID, prompt.String())
}

func (b *Bot) handleIncomingDocument(msg *tgbotapi.Message) {
	doc := msg.Document
	fileName := doc.FileName
	if fileName == "" {
		fileName = "user_document.bin"
	}

	_ = b.sendTyping(msg.Chat.ID)
	statusMsg, _ := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("📄 <i>Загружаю файл '%s'...</i>", html.EscapeString(fileName))))

	timestamp := time.Now().Unix()
	localPath := fmt.Sprintf("/tmp/user_doc_%d_%s", timestamp, filepath.Base(fileName))

	if err := b.downloadTelegramFile(doc.FileID, localPath); err != nil {
		if statusMsg.MessageID > 0 {
			_, _ = b.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, statusMsg.MessageID))
		}
		b.sendText(msg.Chat.ID, fmt.Sprintf("❌ Ошибка сохранения файла: %s", html.EscapeString(err.Error())))
		return
	}

	caption := strings.TrimSpace(msg.Caption)
	if caption == "" {
		caption = "Пользователь загрузил этот файл. Проанализируй его содержимое или структуру."
	}

	prompt := fmt.Sprintf("Пользователь отправил файл '%s' (сохранен на сервере в '%s').\nВопрос / задание пользователя: %s", fileName, localPath, caption)

	if statusMsg.MessageID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, statusMsg.MessageID))
	}

	b.handleAIQuery(msg.Chat.ID, msg.From.ID, prompt)
}

func (b *Bot) downloadTelegramFile(fileID, destPath string) error {
	fileURL, err := b.api.GetFileDirectURL(fileID)
	if err != nil {
		return fmt.Errorf("ошибка получения URL файла: %w", err)
	}

	resp, err := http.Get(fileURL)
	if err != nil {
		return fmt.Errorf("ошибка скачивания файла: %w", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("ошибка создания локального файла: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// ==============================================================================
// Обработка CallbackQuery (Inline Кнопки)
// ==============================================================================

func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
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

	// Interactive AI options (ask_question)
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

	case "act_mail_ai":
		b.handleAIQuery(chatID, userID, "Проанализируй мои последние входящие письма через /opt/superbot/check_mail.py --deep --limit 5: выдели самое важное, что требует внимания, дедлайны и интересные темы.")

	case "act_backup_info":
		b.replyBackupInfo(chatID)

	case "act_create_backup":
		if b.checkAdmin(chatID, userID) {
			b.replyCreateBackup(chatID)
		}

	case "act_errors":
		b.replyRecentErrors(chatID)

	case "act_log_superbot":
		out, err := b.sys.GetServiceLogs("superbot", 20)
		if err != nil {
			b.sendText(chatID, "❌ "+err.Error())
		} else {
			b.sendText(chatID, out)
		}

	case "act_log_agypilot":
		out, err := b.sys.GetServiceLogs("agy-pilot", 20)
		if err != nil {
			b.sendText(chatID, "❌ "+err.Error())
		} else {
			b.sendText(chatID, out)
		}

	case "act_log_omniroute":
		out, err := b.sys.GetServiceLogs("omniroute", 20)
		if err != nil {
			b.sendText(chatID, "❌ "+err.Error())
		} else {
			b.sendText(chatID, out)
		}

	case "act_log_xui":
		out, err := b.sys.GetServiceLogs("x-ui", 20)
		if err != nil {
			b.sendText(chatID, "❌ "+err.Error())
		} else {
			b.sendText(chatID, out)
		}

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

	case "act_prompt_file":
		b.sendText(chatID, "📁 <b>Запрос файлов и папок с сервера:</b>\n\nОтправьте команду:\n<code>/send /путь/к/файлу</code>\nили\n<code>/send /путь/к/папке</code> <i>(папка автоматически упакуется в ZIP)</i>\n\nНапример:\n• <code>/send /etc/os-release</code>\n• <code>/send /opt/superbot</code>")

	case "act_prompt_photo":
		b.sendText(chatID, "📸 <b>Анализ фото, скриншотов и документов:</b>\n\nПросто отправьте в диалог любую фотографию или файл (с подписью или без).\nБот сохранит изображение, распознает текст через OCR и передаст в Antigravity!")

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
			tgbotapi.NewInlineKeyboardButtonData("📬 Почта", "act_mail"),
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус", "act_status"),
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Все функции", "menu_all"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚡ Приоритеты", "menu_prio"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Новый диалог ИИ", "act_new_ai"),
		),
	)
}

func (b *Bot) getPersistentKeyboard() tgbotapi.ReplyKeyboardMarkup {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📬 Почта"),
			tgbotapi.NewKeyboardButton("📊 Статус"),
			tgbotapi.NewKeyboardButton("⚡ Приоритеты"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🧹 Очистить RAM"),
			tgbotapi.NewKeyboardButton("🌐 Сеть & VPN"),
			tgbotapi.NewKeyboardButton("📦 Бэкап"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 Логи"),
			tgbotapi.NewKeyboardButton("🔄 Новый диалог"),
			tgbotapi.NewKeyboardButton("⚙️ Меню"),
		),
	)
	keyboard.ResizeKeyboard = true
	keyboard.InputFieldPlaceholder = "Спросите ИИ или выберите команду..."
	return keyboard
}

func (b *Bot) sendPersistentKeyboard(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = b.getPersistentKeyboard()
	_, _ = b.api.Send(msg)
	// Reset throttle so the keyboard is treated as "just sent"
	b.lastKbSent.Store(chatID, time.Now())
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
			tgbotapi.NewInlineKeyboardButtonData("🖥 Сервер", "menu_server"),
			tgbotapi.NewInlineKeyboardButtonData("🧠 ИИ", "menu_ai"),
			tgbotapi.NewInlineKeyboardButtonData("📁 Файлы", "menu_files"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🌐 Сеть & IP", "menu_net"),
			tgbotapi.NewInlineKeyboardButtonData("📸 Анализ фото", "act_prompt_photo"),
			tgbotapi.NewInlineKeyboardButtonData("📁 Запросить файл", "act_prompt_file"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👑 Пользователи", "menu_users"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "menu_main"),
		),
	)

	text := "⚙️ <b>Все функции сервера:</b>\nВыберите категорию для управления:"
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &markup
	if _, err := b.api.Send(edit); err != nil {
		msg := tgbotapi.NewMessage(chatID, text)
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
			tgbotapi.NewInlineKeyboardButtonData("⚠️ Лог ошибок", "act_errors"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Рестарт OmniRoute", "act_restart_ai"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "menu_all"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "🖥 <b>Управление сервером и памятью:</b>\nВсе функции выполняются мгновенно за 0.05 сек:")
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
			tgbotapi.NewInlineKeyboardButtonData("📦 Бэкап наработок", "act_backup_info"),
			tgbotapi.NewInlineKeyboardButtonData("📂 Список /root", "act_ls_root"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📥 Запросить файл", "act_prompt_file"),
			tgbotapi.NewInlineKeyboardButtonURL("🔗 FileBrowser Web", fileBrowserURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к категориям", "menu_all"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, "📁 <b>Файловый менеджер & Бэкапы:</b>\nСкачивайте бэкапы, просматривайте файлы или откройте веб-интерфейс FileBrowser:")
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

	text := fmt.Sprintf("🧠 <b>Интеллектуальный агент Antigravity:</b>\n%s\n\nМодель: <code>gemini-3.8-flash-high</code>\nМост: <code>agy-pilot (:43430)</code>\nДля общения просто напишите вопрос в чат.", statusText)
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
	_ = b.sendTyping(chatID)
	waitMsg, _ := b.api.Send(tgbotapi.NewMessage(chatID, "📬 <i>Проверяю почтовые ящики (Gmail & Mail.ru)...</i>"))

	out, err := b.sys.CheckMail()
	if waitMsg.MessageID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, waitMsg.MessageID))
	}

	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ <b>Ошибка проверки почты:</b>\n%s", html.EscapeString(err.Error())))
		return
	}

	mailMarkup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "act_mail"),
			tgbotapi.NewInlineKeyboardButtonData("🧠 Анализ входящих (ИИ)", "act_mail_ai"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Главное меню", "menu_main"),
		),
	)

	b.sendHTMLChunked(chatID, out, &mailMarkup)
}

func (b *Bot) replyBackupInfo(chatID int64) {
	out, err := b.sys.GetBackupInfo()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка бэкапа: %s", html.EscapeString(err.Error())))
		return
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Создать свежий бэкап", "act_create_backup"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Главное меню", "menu_main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, out)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = markup
	_, _ = b.api.Send(msg)
}

func (b *Bot) replyCreateBackup(chatID int64) {
	_ = b.sendTyping(chatID)
	waitMsg, _ := b.api.Send(tgbotapi.NewMessage(chatID, "📦 <i>Создаю полный ZIP-архив наработок... (это займет несколько секунд)</i>"))

	out, err := b.sys.CreateBackup()
	if waitMsg.MessageID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, waitMsg.MessageID))
	}

	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка создания бэкапа: %s", html.EscapeString(err.Error())))
		return
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Создать свежий бэкап", "act_create_backup"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Главное меню", "menu_main"),
		),
	)
	b.sendHTMLChunked(chatID, out, &markup)
}

func (b *Bot) replyRecentErrors(chatID int64) {
	out, err := b.sys.GetRecentErrors()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка чтения журнала: %s", html.EscapeString(err.Error())))
		return
	}
	b.sendText(chatID, out)
}

func (b *Bot) replyLogsMenu(chatID int64) {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🤖 SuperBot", "act_log_superbot"),
			tgbotapi.NewInlineKeyboardButtonData("🌉 AGY-Pilot", "act_log_agypilot"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🌐 OmniRoute", "act_log_omniroute"),
			tgbotapi.NewInlineKeyboardButtonData("🔒 X-UI VPN", "act_log_xui"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚠️ Системные ошибки", "act_errors"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "📋 <b>Логи сервисов</b>\nВыберите сервис (последние 20 строк):")
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = markup
	_, _ = b.api.Send(msg)
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

func (b *Bot) replySendFile(chatID int64, rawPath string) {
	filePath, isZip, err := formatter.PrepareFileForSending(rawPath)
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ %s", html.EscapeString(err.Error())))
		return
	}

	if isZip {
		defer os.Remove(filePath)
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	docName := filepath.Base(filePath)
	if isZip {
		doc.Caption = fmt.Sprintf("📦 <b>ZIP-архив папки:</b> <code>%s</code>", html.EscapeString(rawPath))
	} else {
		doc.Caption = fmt.Sprintf("📄 <code>%s</code>", html.EscapeString(rawPath))
	}
	doc.ParseMode = "HTML"

	if _, err := b.api.Send(doc); err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ Ошибка отправки документа '%s': %s", docName, html.EscapeString(err.Error())))
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
	sb.WriteString("• /send &lt;путь&gt; — Скачать файл или ZIP папки в Telegram\n")
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
	sb.WriteString("<i>Отправьте любой текст или фото, чтобы передать задачу в Antigravity.</i>")

	b.sendTextWithMenu(chatID, sb.String())
}

func (b *Bot) checkAdmin(chatID, userID int64) bool {
	if !b.auth.IsAdmin(userID) {
		b.sendText(chatID, "⛔ Эта команда доступна только администраторам сервера.")
		return false
	}
	return true
}

func (b *Bot) sendTyping(chatID int64) error {
	_, err := b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
	return err
}

// ==============================================================================
// Antigravity AI Bridge, Interactive Options & File Delivery
// ==============================================================================

func (b *Bot) handleAIQuery(chatID, userID int64, prompt string) {
	_ = b.sendTyping(chatID)

	waitMsg, err := b.api.Send(tgbotapi.NewMessage(chatID, "⏳ <i>Думаю и выполняю задачу...</i>"))
	waitMsgID := 0
	if err == nil {
		waitMsgID = waitMsg.MessageID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()

	// Keep typing indicator active while Antigravity thinks and executes tool calls
	stopTyping := make(chan struct{})
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopTyping:
				return
			case <-ticker.C:
				_ = b.sendTyping(chatID)
			}
		}
	}()
	defer close(stopTyping)

	// Rate-limited real-time progress updates to Telegram
	var lastEdit time.Time
	var lastSummary string
	var editMu sync.Mutex

	onProgress := func(action, summary string) {
		if waitMsgID <= 0 || summary == "" {
			return
		}
		editMu.Lock()
		defer editMu.Unlock()

		if summary == lastSummary {
			return
		}
		lastSummary = summary

		// Telegram rate-limiting: at most 1 edit every 1800ms
		if time.Since(lastEdit) < 1800*time.Millisecond {
			return
		}
		lastEdit = time.Now()

		statusText := fmt.Sprintf("⏳ <b>В процессе:</b>\n└ ⚡ <i>%s...</i>", html.EscapeString(summary))
		edit := tgbotapi.NewEditMessageText(chatID, waitMsgID, statusText)
		edit.ParseMode = "HTML"
		_, _ = b.api.Send(edit)
	}

	activeConvID := b.getSession(userID)
	resp, err := b.aiCli.AskStream(ctx, prompt, activeConvID, false, onProgress)
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

	rawReply := resp.Text
	if rawReply == "" && resp.Question != "" {
		rawReply = "❓ <b>" + html.EscapeString(resp.Question) + "</b>"
	}

	// Format reply: Convert Markdown to clean Telegram HTML & extract server files
	formatted := formatter.ConvertMarkdownToTelegramHTML(rawReply)

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

	// Remove wait message
	if waitMsgID > 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, waitMsgID))
	}

	// Deliver reply safely
	b.sendHTMLChunked(chatID, formatted.HTMLText, optMarkup)

	// Automatically send any server files referenced in the response (e.g. file:///etc/os-release)
	for _, fPath := range formatted.Files {
		if _, err := os.Stat(fPath); err == nil {
			b.replySendFile(chatID, fPath)
		}
	}
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
	const kbThrottle = 10 * time.Minute

	// Decide whether to attach the persistent keyboard this time.
	// Only re-send it if it hasn't been sent to this chat in the last 10 minutes.
	shouldSendKb := func() bool {
		if markup != nil {
			return false // inline markup takes priority, skip persistent kb
		}
		now := time.Now()
		if last, ok := b.lastKbSent.Load(chatID); ok {
			if now.Sub(last.(time.Time)) < kbThrottle {
				return false
			}
		}
		b.lastKbSent.Store(chatID, now)
		return true
	}

	if len(text) <= maxChunk {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "HTML"
		if markup != nil {
			msg.ReplyMarkup = markup
		} else if shouldSendKb() {
			kb := b.getPersistentKeyboard()
			msg.ReplyMarkup = kb
		}
		if _, err := b.api.Send(msg); err != nil {
			msg.ParseMode = ""
			_, _ = b.api.Send(msg)
		}
		return
	}

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
		} else if shouldSendKb() {
			kb := b.getPersistentKeyboard()
			msg.ReplyMarkup = kb
		}
		if _, err := b.api.Send(msg); err != nil {
			msg.ParseMode = ""
			_, _ = b.api.Send(msg)
		}
	}
}
