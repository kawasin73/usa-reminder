package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"

	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/pkg/errors"
)

var timeMatcher = regexp.MustCompile("([0-9]+)時([0-9]+)分")

type Handler struct {
	bot   *linebot.Client
	store *Store
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	events, err := h.bot.ParseRequest(req)
	if err != nil {
		if err == linebot.ErrInvalidSignature {
			w.WriteHeader(400)
		} else {
			w.WriteHeader(500)
		}
		return
	}
	for _, event := range events {
		if event.Type == linebot.EventTypeMessage {
			switch message := event.Message.(type) {
			case *linebot.TextMessage:
				if err = h.onTextMessageEvent(event, message); err != nil {
					log.Println(err)
				}
			}
		}
	}
}

func (h *Handler) onTextMessageEvent(event linebot.Event, msg *linebot.TextMessage) error {
	reply, _ := h.handleText(event.Source.UserID, msg.Text)
	if _, err := h.bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(reply)).Do(); err != nil {
		return err
	}
	return nil
}

func (h *Handler) handleText(userId, text string) (string, error) {
	// TODO: create command
	if text == "設定教えて" {
		user := h.store.Get(userId)
		if user == nil {
			return "設定されてないですよ", nil
		}
		return fmt.Sprintf("%v時%v分に設定されています", user.Hour, user.Minute), nil
	}
	if text == "ばいばい" {
		if h.store.Del(userId) {
			return "設定を削除しました。 ばいばい", nil
		} else {
			return "設定されてないですよ", nil
		}
	}
	m := timeMatcher.FindStringSubmatch(text)
	if len(m) == 3 {
		hour, err := strconv.Atoi(m[1])
		if err != nil {
			return "何時ですか？", errors.Wrap(err, "parse hour")
		}
		minute, err := strconv.Atoi(m[2])
		if err != nil {
			return "何分ですか？", errors.Wrap(err, "parse minute")
		}
		err = h.store.Set(userId, hour, minute)
		if err != nil {
			return "時間の設定に失敗しました", errors.Wrap(err, "set time to store")
		}
		return fmt.Sprintf("%v時%v分ですね。わかりました。", hour, minute), nil
	}
	user := h.store.Get(userId)
	if user == nil {
		return "時間を設定してください", nil
	}
	if user.ResetCount() {
		return "よくできました", nil
	} else {
		// TODO: send random message
		return text, nil
	}
}
