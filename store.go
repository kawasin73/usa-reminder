package main

import (
	"strconv"
	"strings"
	"sync"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
)

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
