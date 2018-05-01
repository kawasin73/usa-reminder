package main

import (
	"context"
	"sync"
	"time"

	"github.com/line/line-bot-sdk-go/linebot"
)

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
