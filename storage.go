package main

import (
	"gopkg.in/redis.v4"
	"time"
)

type RedisStorage struct {
	client *redis.Client
}

func (rs *RedisStorage) HealthCheck() error {
	_, err := rs.client.Info("all").Result()
	return err
}

func (rs *RedisStorage) Init(addr string) error {
	rs.client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "",
		DB:       0,
	})
	return nil
}

func (rs *RedisStorage) Set(key string, value string, duration time.Duration) (string, error) {
	return rs.client.Set(key, value, duration).Result()
}

func (rs *RedisStorage) Get(key string) (string, error) {
	return rs.client.Get(key).Result()
}


func (rs *RedisStorage) GetRoutes() ([]string, error) {
	return rs.client.LRange("routes", 0, -1).Result()
}

func (rs *RedisStorage) RemoveRoute(bytes string) error {
	_, err := rs.client.LRem("routes", 1, bytes).Result()
	return err
}

func (rs *RedisStorage) AddRoute(bytes string) error {
	_, err := rs.client.RPush("routes", bytes).Result()
	return err
}
