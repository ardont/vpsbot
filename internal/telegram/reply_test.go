package telegram

import (
	"encoding/json"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestBuildPromptWithQuote(t *testing.T) {
	msg := &IncomingMessage{
		Message: tgbotapi.Message{
			Text: "А он сильно шумит?",
			ReplyToMessage: &tgbotapi.Message{
				Text: "Acer Swift Go 14 AI SFG14-75-58NB: Intel Core Ultra 5 226V, 16 ГБ ОЗУ, 1 ТБ SSD",
				From: &tgbotapi.User{
					UserName: "superbot",
					IsBot:    true,
				},
			},
		},
		Quote: &TextQuote{
			Text: "Intel Core Ultra 5 226V, 16 ГБ ОЗУ",
		},
	}

	prompt := buildPromptWithReplyContext(msg, "superbot")

	if !strings.Contains(prompt, "💬 [Пользователь ответил с цитатой из сообщения]:") {
		t.Fatalf("expected quote header, got: %s", prompt)
	}
	if !strings.Contains(prompt, "> Intel Core Ultra 5 226V, 16 ГБ ОЗУ") {
		t.Fatalf("expected quoted text, got: %s", prompt)
	}
	if !strings.Contains(prompt, "А он сильно шумит?") {
		t.Fatalf("expected user question, got: %s", prompt)
	}
	if !strings.Contains(prompt, "ты (бот SuperBot / Antigravity)") {
		t.Fatalf("expected bot author attribution, got: %s", prompt)
	}
}

func TestBuildPromptWithFullReply(t *testing.T) {
	msg := &IncomingMessage{
		Message: tgbotapi.Message{
			Text: "Сколько стоит в DNS?",
			ReplyToMessage: &tgbotapi.Message{
				Text: "Модель: Acer Aspire Go 15 AG15-31P с процессором Core i3-N305",
				From: &tgbotapi.User{
					UserName:  "alex",
					FirstName: "Alex",
				},
			},
		},
	}

	prompt := buildPromptWithReplyContext(msg, "superbot")

	if !strings.Contains(prompt, "💬 [Пользователь ответил на сообщение от @alex]:") {
		t.Fatalf("expected reply header, got: %s", prompt)
	}
	if !strings.Contains(prompt, "> Модель: Acer Aspire Go 15") {
		t.Fatalf("expected original text, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Сколько стоит в DNS?") {
		t.Fatalf("expected user question, got: %s", prompt)
	}
}

func TestBuildPromptWithoutReply(t *testing.T) {
	msg := &IncomingMessage{
		Message: tgbotapi.Message{
			Text: "Покажи статус сервера",
		},
	}

	prompt := buildPromptWithReplyContext(msg, "superbot")
	if prompt != "Покажи статус сервера" {
		t.Fatalf("expected clean text, got: %s", prompt)
	}
}

func TestIncomingUpdateJSONDecoding(t *testing.T) {
	raw := `[
		{
			"update_id": 9991,
			"message": {
				"message_id": 777,
				"chat": {"id": 12345, "type": "private"},
				"from": {"id": 12345, "username": "user1"},
				"text": "Уточни детали",
				"reply_to_message": {
					"message_id": 776,
					"text": "Предыдущее сообщение"
				},
				"quote": {
					"text": "фрагмент цитаты",
					"is_manual": true
				}
			}
		}
	]`

	var updates []IncomingUpdate
	if err := json.Unmarshal([]byte(raw), &updates); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	u := updates[0]
	if u.Message == nil {
		t.Fatal("expected message to be present")
	}
	if u.Message.Quote == nil || u.Message.Quote.Text != "фрагмент цитаты" {
		t.Fatalf("unexpected quote: %+v", u.Message.Quote)
	}
	if u.Message.ReplyToMessage == nil || u.Message.ReplyToMessage.Text != "Предыдущее сообщение" {
		t.Fatalf("unexpected reply_to_message: %+v", u.Message.ReplyToMessage)
	}
}

func TestServicesMenuMarkup(t *testing.T) {
	bot := &Bot{}
	markup := bot.getServicesMenuMarkup()

	expectedURLs := map[string]string{
		"📁 FileBrowser":   "http://100.89.7.20:8080",
		"🌐 OmniRoute":     "http://100.89.7.20:20128",
		"🔒 Панель 3X-UI":  "http://100.89.7.20:53395",
		"🔑 Antigravity IDE": "https://vm-576151.tailb9b76e.ts.net:43429",
	}

	found := make(map[string]bool)
	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			if url, ok := expectedURLs[btn.Text]; ok {
				if btn.URL == nil || *btn.URL != url {
					t.Errorf("button %s expected URL %s, got %v", btn.Text, url, btn.URL)
				}
				found[btn.Text] = true
			}
		}
	}

	for name := range expectedURLs {
		if !found[name] {
			t.Errorf("expected button %s in services markup, but not found", name)
		}
	}
}
