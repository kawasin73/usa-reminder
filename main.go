package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"

	"github.com/line/line-bot-sdk-go/linebot"
)

var timeMatcher = regexp.MustCompile("([0-9]+)時([0-9]+)分")

func main() {
	bot, err := linebot.New(
		os.Getenv("CHANNEL_SECRET"),
		os.Getenv("CHANNEL_TOKEN"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Setup HTTP Server for receiving requests from LINE platform
	http.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		events, err := bot.ParseRequest(req)
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
					if err = onTextMessageEvent(bot, event, message); err != nil {
						log.Println(err)
					}
				}
			}
		}
	})
	// This is just sample code.
	// For actual use, you must support HTTPS by using `ListenAndServeTLS`, a reverse proxy or something else.
	if err := http.ListenAndServe(":"+os.Getenv("PORT"), nil); err != nil {
		log.Fatal(err)
	}
}

func onTextMessageEvent(bot *linebot.Client, event linebot.Event, msg *linebot.TextMessage) error {
	m := timeMatcher.FindStringSubmatch(msg.Text)
	var reply string
	if len(m) == 3 {
		reply = fmt.Sprintf("%v時%v分ですね。わかりました。", m[1], m[2])
	} else {
		reply = msg.Text
	}
	if _, err := bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(reply)).Do(); err != nil {
		return err
	}
	return nil
}
