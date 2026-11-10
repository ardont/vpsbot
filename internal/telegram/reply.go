package telegram

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TextQuote represents the quoted portion of a message (Telegram Bot API 7.0+).
type TextQuote struct {
	Text     string `json:"text"`
	Position int    `json:"position,omitempty"`
	IsManual bool   `json:"is_manual,omitempty"`
}

// IncomingMessage wraps tgbotapi.Message with native Telegram quote support.
type IncomingMessage struct {
	tgbotapi.Message
	Quote *TextQuote `json:"quote,omitempty"`
}

// IncomingUpdate wraps tgbotapi.Update with custom Message decoding.
type IncomingUpdate struct {
	UpdateID      int                     `json:"update_id"`
	Message       *IncomingMessage        `json:"message,omitempty"`
	EditedMessage *IncomingMessage        `json:"edited_message,omitempty"`
	CallbackQuery *tgbotapi.CallbackQuery `json:"callback_query,omitempty"`
}

// formatAuthor returns a human-friendly representation of the replied message sender.
func formatAuthor(from *tgbotapi.User, botUsername string) string {
	if from == nil {
		return "собеседник"
	}
	if from.IsBot || (botUsername != "" && strings.EqualFold(from.UserName, botUsername)) {
		return "ты (бот SuperBot / Antigravity)"
	}
	if from.UserName != "" {
		return "@" + from.UserName
	}
	name := strings.TrimSpace(from.FirstName + " " + from.LastName)
	if name != "" {
		return name
	}
	return "пользователь"
}

// buildPromptWithReplyContext combines user text with quoted or replied message context.
func buildPromptWithReplyContext(msg *IncomingMessage, botUsername string) string {
	text := strings.TrimSpace(msg.Text)
	var sb strings.Builder

	// 1. Quoted excerpt (Telegram quote reply)
	if msg.Quote != nil && strings.TrimSpace(msg.Quote.Text) != "" {
		quoted := strings.TrimSpace(msg.Quote.Text)
		sb.WriteString("💬 [Пользователь ответил с цитатой из сообщения]:\n")
		for _, line := range strings.Split(quoted, "\n") {
			sb.WriteString("> " + line + "\n")
		}
		if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
			sb.WriteString(fmt.Sprintf("(Источник цитаты: %s)\n", formatAuthor(msg.ReplyToMessage.From, botUsername)))
		}
		sb.WriteString("\nВопрос или уточнение пользователя к этой цитате:\n")
		sb.WriteString(text)
		return sb.String()
	}

	// 2. Full message reply (Swipe to reply)
	if msg.ReplyToMessage != nil {
		replyText := strings.TrimSpace(msg.ReplyToMessage.Text)
		if replyText == "" {
			replyText = strings.TrimSpace(msg.ReplyToMessage.Caption)
		}
		if replyText == "" {
			if len(msg.ReplyToMessage.Photo) > 0 {
				replyText = "[Изображение / Фото]"
			} else if msg.ReplyToMessage.Document != nil {
				replyText = fmt.Sprintf("[Документ: %s]", msg.ReplyToMessage.Document.FileName)
			}
		}

		if replyText != "" {
			// Limit reply text length to avoid token explosion
			if len(replyText) > 1200 {
				replyText = replyText[:1200] + "\n...(сокращено)..."
			}
			author := formatAuthor(msg.ReplyToMessage.From, botUsername)
			sb.WriteString(fmt.Sprintf("💬 [Пользователь ответил на сообщение от %s]:\n", author))
			for _, line := range strings.Split(replyText, "\n") {
				sb.WriteString("> " + line + "\n")
			}
			sb.WriteString("\nВопрос или уточнение пользователя:\n")
			sb.WriteString(text)
			return sb.String()
		}
	}

	return text
}

// getReplyContextSnippet extracts reply/quote context for multimodal prompts (photos/documents).
func getReplyContextSnippet(msg *IncomingMessage, botUsername string) string {
	if msg.Quote != nil && strings.TrimSpace(msg.Quote.Text) != "" {
		var sb strings.Builder
		sb.WriteString("💬 [В ответ на цитату]:\n")
		for _, line := range strings.Split(strings.TrimSpace(msg.Quote.Text), "\n") {
			sb.WriteString("> " + line + "\n")
		}
		return sb.String()
	}

	if msg.ReplyToMessage != nil {
		replyText := strings.TrimSpace(msg.ReplyToMessage.Text)
		if replyText == "" {
			replyText = strings.TrimSpace(msg.ReplyToMessage.Caption)
		}
		if replyText != "" {
			if len(replyText) > 800 {
				replyText = replyText[:800] + "\n...(сокращено)..."
			}
			author := formatAuthor(msg.ReplyToMessage.From, botUsername)
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("💬 [В ответ на сообщение от %s]:\n", author))
			for _, line := range strings.Split(replyText, "\n") {
				sb.WriteString("> " + line + "\n")
			}
			return sb.String()
		}
	}

	return ""
}
