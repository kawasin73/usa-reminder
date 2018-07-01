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

var (
	timeMatcher    = regexp.MustCompile("([0-9]+)時([0-9]+)分")
	time2Matcher   = regexp.MustCompile("([0-9]+)[:|：]([0-9]+)")
	deleteMatcher  = regexp.MustCompile("削除|解除")
	doneMatcher    = regexp.MustCompile("飲んだ|のんだ|はい|うん|のみ|飲み|OK|ok|おっけ|オッケ|もち")
	NotTimeCommand = errors.New("not time command")
)

func parseTime(text string) (hour, minute int, err error) {
	m := timeMatcher.FindStringSubmatch(text)
	if len(m) != 3 {
		m = time2Matcher.FindStringSubmatch(text)
	}
	if len(m) != 3 {
		return 0, 0, NotTimeCommand
	}
	hour, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, errors.Wrap(err, "parse hour")
	}
	minute, err = strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, errors.Wrap(err, "parse minute")
	}
	return hour, minute, nil
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
	if deleteMatcher.MatchString(text) {
		err := h.store.Del(userId)
		if err == ErrNotFound {
			return "設定されてないですよ", nil
		} else if err != nil {
			return "設定の削除に失敗しました", err
		}
		return "設定を削除しました。 ばいばい", nil
	}
	if hour, minute, err := parseTime(text); err == nil {
		err = h.store.Set(userId, hour, minute)
		if err != nil {
			return "時間の設定に失敗しました", errors.Wrap(err, "set time to store")
		}
		return fmt.Sprintf("%v時%v分ですね。わかりました。", hour, minute), nil
	} else if err != NotTimeCommand {
		return "時間がおかしいよ", err
	}
	user := h.store.Get(userId)
	if user == nil {
		return "時間を設定してください", nil
	}
	if doneMatcher.MatchString(text) && user.ResetCount() {
		return "よくできました", nil
	}
	// TODO: send random message
	return text, nil
}
