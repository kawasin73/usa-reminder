package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/pkg/errors"
)

type User struct {
	Id     string
	Hour   int
	Minute int

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	sent   int
}

func NewUser(ctx context.Context, id string, hour, minute int) *User {
	user := &User{
		Id:     id,
		Hour:   hour,
		Minute: minute,
	}
	user.ctx, user.cancel = context.WithCancel(ctx)
	return user
}

func (u *User) Data() string {
	return fmt.Sprintf("%d:%d", u.Hour, u.Minute)
}

func (u *User) ResetCount() (reset bool) {
	u.mu.Lock()
	reset = u.sent != 0
	u.sent = 0
	u.mu.Unlock()
	return
}

func (u *User) nextTime(now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), u.Hour, u.Minute, 0, 0, time.Local)
	if t.Before(now) {
		t = t.Add(time.Hour * 24)
	}
	return t
}

func (u *User) SendFirst(bot *linebot.Client) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sent = 1
	_, err := bot.PushMessage(u.Id, linebot.NewTextMessage("飲んだ?")).Do()
	if err != nil {
		u.sent = 0
		log.Println(errors.Wrapf(err, "failed push message to (%v)", u.Id))
	}
}

func (u *User) SendRemind(bot *linebot.Client) (tryNext bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.sent == 0 {
		// have received response
		return false
	}
	if u.sent > maxRetry {
		// stop remind roop and wait for reply
		return false
	}
	u.sent++
	_, err := bot.PushMessage(u.Id, linebot.NewTextMessage("飲んだ"+strings.Repeat("?", u.sent))).Do()
	if err != nil {
		log.Println(errors.Wrapf(err, "failed push message to (%v)", u.Id))
	}
	return true
}

func (u *User) Close() {
	u.cancel()
}
