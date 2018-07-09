package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/pkg/errors"
)

type User struct {
	Id       string   `json:"id"`
	Hour     int      `json:"hour"`
	Minute   int      `json:"minute"`
	Notifies []Notify `json:"notifies"`

	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	sent        int
	tmpNotifyId string
}

type Notify struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func NewUser(ctx context.Context, id string, hour, minute int, prevUser *User) *User {
	user := &User{
		Id:     id,
		Hour:   hour,
		Minute: minute,
	}
	user.ctx, user.cancel = context.WithCancel(ctx)
	if prevUser != nil {
		prevUser.mu.Lock()
		user.Notifies = append(user.Notifies, prevUser.Notifies...)
		prevUser.mu.Unlock()
	}
	return user
}

func DecodeUser(ctx context.Context, data string) (*User, error) {
	user := new(User)
	err := json.Unmarshal([]byte(data), user)
	if err != nil {
		return nil, err
	}
	user.ctx, user.cancel = context.WithCancel(ctx)
	return user, nil
}

func (u *User) Data() (string, error) {
	data, err := json.Marshal(u)
	return string(data), err
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
		u.notifyNotDone(bot)
		return false
	}
	u.sent++
	_, err := bot.PushMessage(u.Id, linebot.NewTextMessage("飲んだ"+strings.Repeat("?", u.sent))).Do()
	if err != nil {
		log.Println(errors.Wrapf(err, "failed push message to (%v)", u.Id))
	}
	return true
}

func (u *User) SetNotifyId(id string) {
	u.mu.Lock()
	u.tmpNotifyId = id
	u.mu.Unlock()
}

func (u *User) SetNotifyName(name string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tmpNotifyId == "" {
		return false
	}
	// search duplicated id
	for i, n := range u.Notifies {
		if n.Id == u.tmpNotifyId {
			u.Notifies[i].Name = name
			u.tmpNotifyId = ""
			return true
		}
	}
	u.Notifies = append(u.Notifies, Notify{Id: u.tmpNotifyId, Name: name})
	u.tmpNotifyId = ""
	return true
}

func (u *User) NotifyDone(bot *linebot.Client) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, n := range u.Notifies {
		if _, err := bot.PushMessage(n.Id, linebot.NewTextMessage(n.Name+" が今飲んだよ！")).Do(); err != nil {
			log.Println("failed to notify not done : ", err)
		}
	}
}

func (u *User) notifyNotDone(bot *linebot.Client) {
	for _, n := range u.Notifies {
		if _, err := bot.PushMessage(n.Id, linebot.NewTextMessage(n.Name+" は今日まだ飲んでないよ〜")).Do(); err != nil {
			log.Println("failed to notify not done : ", err)
		}
	}
}

func (u *User) Close() {
	u.cancel()
}
