package main

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
)

var (
	ErrNotFound = errors.New("user not found")
)

type Store struct {
	db   *redis.Client
	mu   sync.Mutex
	data map[string]*User
	sche *Scheduler

	ctx context.Context
	wg  *sync.WaitGroup
}

func NewStore(ctx context.Context, client *redis.Client, wg *sync.WaitGroup, sche *Scheduler) *Store {
	return &Store{
		db:   client,
		wg:   wg,
		sche: sche,
		ctx:  ctx,
	}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys, err := s.db.Keys(userPrefix + "*").Result()
	if err != nil {
		return errors.Wrap(err, "get all key")
	}
	users := make(map[string]*User, len(keys))

	for _, key := range keys {
		// TODO: MGET
		data, err := s.db.Get(key).Result()
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
		user := NewUser(s.ctx, key[len(userPrefix):], hour, minute, nil)
		users[user.Id] = user
	}

	for _, user := range s.data {
		user.Close()
	}

	s.data = users

	for _, user := range users {
		s.sche.InitRemind(user)
	}

	return nil
}

func (s *Store) Set(userId string, hour, minute int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prevUser, _ := s.data[userId]
	user := NewUser(s.ctx, userId, hour, minute, prevUser)
	_, err := s.db.Set(userPrefix+user.Id, user.Data(), 0).Result()
	if err != nil {
		return errors.Wrap(err, "set to redis")
	}
	if prevUser != nil {
		prevUser.Close()
	}
	s.data[user.Id] = user
	s.sche.InitRemind(user)
	return nil
}

func (s *Store) Get(userId string) *User {
	s.mu.Lock()
	user, _ := s.data[userId]
	s.mu.Unlock()
	return user
}

func (s *Store) Del(userId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.data[userId]
	if !ok {
		return ErrNotFound
	}
	if _, err := s.db.Del(userPrefix + user.Id).Result(); err != nil {
		return errors.Wrapf(err, "del from redis")
	}
	user.Close()
	delete(s.data, userId)
	return nil
}
