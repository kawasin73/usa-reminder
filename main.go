package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/pkg/errors"
)

const (
	userPrefix = "user_"
	location   = "Asia/Tokyo"
	maxRetry   = 10
)

func init() {
	loc, err := time.LoadLocation(location)
	if err != nil {
		loc = time.FixedZone(location, 9*60*60)
	}
	time.Local = loc
}

var timeMatcher = regexp.MustCompile("([0-9]+)時([0-9]+)分")

type Store struct {
	c    *redis.Client
	mu   sync.Mutex
	data map[string]*User
	sche *Scheduler

	wg *sync.WaitGroup
}

func NewStore(client *redis.Client, wg *sync.WaitGroup, sche *Scheduler) *Store {
	return &Store{
		c:    client,
		wg:   wg,
		sche: sche,
	}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys, err := s.c.Keys(userPrefix + "*").Result()
	if err != nil {
		return errors.Wrap(err, "get all key")
	}
	users := make(map[string]*User, len(keys))

	for _, key := range keys {
		// TODO: MGET
		data, err := s.c.Get(key).Result()
		if err != nil {
			return errors.Wrap(err, "get all key")
		}
		times := strings.Split(data, ":")
		hour, err := strconv.Atoi(times[0])
		if err != nil {
			return errors.Wrap(err, "parse hour")
		}
		minute, err := strconv.Atoi(times[1])
		if err != nil {
			return errors.Wrap(err, "parse minute")
		}
		user := NewUser(key[len(userPrefix):], hour, minute)
		users[user.Id] = user
	}

	for _, user := range s.data {
		user.Close()
	}

	s.data = users

	for _, user := range users {
		s.wg.Add(1)
		go s.sche.Watch(s.wg, user)
	}

	return nil
}

func (s *Store) Set(userId string, hour, minute int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := NewUser(userId, hour, minute)
	_, err := s.c.Set(userPrefix+user.Id, user.Data(), 0).Result()
	if err != nil {
		errors.Wrap(err, "set to redis")
	}
	if old, ok := s.data[user.Id]; ok {
		old.Close()
	}
	s.data[user.Id] = user
	s.wg.Add(1)
	go s.sche.Watch(s.wg, user)
	return nil
}

func (s *Store) Get(userId string) *User {
	s.mu.Lock()
	user, _ := s.data[userId]
	s.mu.Unlock()
	return user
}

func (s *Store) Del(userId string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.data[userId]
	if !ok {
		return false
	}
	user.Close()
	delete(s.data, userId)
	return true
}

type User struct {
	Id     string
	Hour   int
	Minute int

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	sent   int
}

func NewUser(id string, hour, minute int) *User {
	user := &User{
		Id:     id,
		Hour:   hour,
		Minute: minute,
	}
	user.ctx, user.cancel = context.WithCancel(context.Background())
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
		u.sent = 0
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

type Scheduler struct {
	bot      *linebot.Client
	chRemind chan *User
}

func (s *Scheduler) Watch(wg *sync.WaitGroup, u *User) {
	defer wg.Done()
	for {
		now := time.Now()
		t := time.Date(now.Year(), now.Month(), now.Day(), u.Hour, u.Minute, 0, 0, time.Local)
		if t.Before(now) {
			t = t.Add(time.Hour * 24)
		}
		select {
		case <-u.ctx.Done():
			return
		case <-time.After(t.Sub(time.Now())):
		}

		u.SendFirst(s.bot)

		select {
		case <-u.ctx.Done():
			return
		case s.chRemind <- u:
		}
	}
}

type Remind struct {
	u     *User
	timer <-chan time.Time
}

func (s *Scheduler) Reminder(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	queue := make([]Remind, 0)
	var remind Remind
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-s.chRemind:
			r := Remind{u: u, timer: time.After(10 * time.Minute)}
			if remind.u == nil {
				remind = r
				continue
			}
			queue = append(queue, r)
		case <-remind.timer:
			if remind.u.SendRemind(s.bot) {
				// requeue
				queue = append(queue, Remind{u: remind.u, timer: time.After(10 * time.Minute)})
			}

			if len(queue) == 0 {
				remind.u, remind.timer = nil, nil
				continue
			}
			// deque
			remind, queue = queue[0], queue[1:]
		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	wg := new(sync.WaitGroup)
	defer func() {
		cancel()
		wg.Wait()
	}()
	bot, err := linebot.New(
		os.Getenv("CHANNEL_SECRET"),
		os.Getenv("CHANNEL_TOKEN"),
	)
	if err != nil {
		log.Fatal(err)
	}
	redisUrl, err := url.Parse(os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal("parse redis url : ", err)
	}
	redisPassword, _ := redisUrl.User.Password()
	redisDB := 0
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisUrl.Host,
		Password: redisPassword,
		DB:       redisDB,
	})

	scheduler := &Scheduler{bot: bot, chRemind: make(chan *User)}
	wg.Add(1)
	go scheduler.Reminder(ctx, wg)

	store := NewStore(redisClient, wg, scheduler)
	if err = store.Load(); err != nil {
		log.Fatal("load redis data : ", err)
	}

	h := &Handler{
		bot:   bot,
		store: store,
	}
	// Setup HTTP Server for receiving requests from LINE platform
	http.Handle("/callback", h)

	// This is just sample code.
	// For actual use, you must support HTTPS by using `ListenAndServeTLS`, a reverse proxy or something else.
	if err := http.ListenAndServe(":"+os.Getenv("PORT"), nil); err != nil {
		log.Fatal(err)
	}
}

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
	reply, _ := h.parseToReply(event.Source.UserID, msg.Text)
	if _, err := h.bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(reply)).Do(); err != nil {
		return err
	}
	return nil
}

func (h *Handler) parseToReply(userId, text string) (string, error) {
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
