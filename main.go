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

	wg  *sync.WaitGroup
	bot *linebot.Client
}

func NewStore(client *redis.Client, wg *sync.WaitGroup, bot *linebot.Client) *Store {
	return &Store{
		c:   client,
		wg:  wg,
		bot: bot,
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
		go Watch(s.wg, s.bot, user)
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
	go Watch(s.wg, s.bot, user)
	return nil
}

func (s *Store) Get(userId string) *User {
	s.mu.Lock()
	user, _ := s.data[userId]
	s.mu.Unlock()
	return user
}

type User struct {
	Id     string
	Hour   int
	Minute int

	ctx    context.Context
	cancel context.CancelFunc
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

func (u *User) Close() {
	u.cancel()
}

func Watch(wg *sync.WaitGroup, bot *linebot.Client, u *User) {
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
		case <-time.After(time.Now().Sub(t)):
		}

		_, err := bot.PushMessage(u.Id, linebot.NewTextMessage("飲んだ?")).Do()
		if err != nil {
			log.Println(errors.Wrapf(err, "failed push message to (%v)", u.Id))
		}
	}
}

func main() {
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
	wg := new(sync.WaitGroup)
	store := NewStore(redisClient, wg, bot)
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
	return text, errors.New("invalid message")
}
