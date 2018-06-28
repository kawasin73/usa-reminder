package main

import (
	"time"

	"github.com/kawasin73/htask"
	"github.com/line/line-bot-sdk-go/linebot"
)

type Scheduler struct {
	bot       *linebot.Client
	scheduler *htask.Scheduler
}

func (s *Scheduler) InitRemind(u *User) {
	s.scheduler.Set(u.ctx.Done(), u.nextTime(time.Now()), s.remindTask(u))
}

func (s *Scheduler) remindTask(u *User) func(time.Time) {
	return func(t time.Time) {
		u.SendFirst(s.bot)
		s.scheduler.Set(u.ctx.Done(), u.nextTime(t), s.remindTask(u))
		s.scheduler.Set(u.ctx.Done(), t.Add(10*time.Minute), s.snooze(u))
	}
}

func (s *Scheduler) snooze(u *User) func(time.Time) {
	return func(t time.Time) {
		if u.SendRemind(s.bot) {
			s.scheduler.Set(u.ctx.Done(), t.Add(10*time.Minute), s.snooze(u))
		}
	}
}
