package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/kawasin73/htask"
	"github.com/line/line-bot-sdk-go/linebot"
)

const (
	location = "Asia/Tokyo"
	maxRetry = 10
)

func init() {
	loc, err := time.LoadLocation(location)
	if err != nil {
		loc = time.FixedZone(location, 9*60*60)
	}
	time.Local = loc
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	wg := new(sync.WaitGroup)
	hsched := htask.NewScheduler(wg, 0)
	defer func() {
		cancel()
		hsched.Close()
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

	scheduler := &Scheduler{bot: bot, scheduler: hsched}

	store := NewStore(ctx, redisClient, wg, scheduler)
	if err = store.Migrate(); err != nil {
		log.Fatal("migration failed : ", err)
	}
	if err = store.Load(); err != nil {
		log.Fatal("load redis data : ", err)
	}

	h := &Handler{
		bot:   bot,
		store: store,
	}
	// Setup HTTP Server for receiving requests from LINE platform
	http.Handle("/callback", h)
	http.HandleFunc("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		_, err := io.WriteString(w, "OK")
		if err != nil {
			log.Println("failed to write health OK", err)
		}
	})

	// This is just sample code.
	// For actual use, you must support HTTPS by using `ListenAndServeTLS`, a reverse proxy or something else.
	if err := http.ListenAndServe(":"+os.Getenv("PORT"), nil); err != nil {
		log.Fatal(err)
	}
}
