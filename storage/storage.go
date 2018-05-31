package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"gopkg.in/redis.v4"
	"time"
	_ "github.com/lib/pq"
)

type Route struct {
	Id             string    `json:"id"`
	Space          string    `json:"space"`
	App            string    `json:"app"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
	DestinationUrl string    `json:"url"`
}

type LogSession struct {
	App   string `json:"app"`
	Space string `json:"space"`
	Lines int    `json:lines`
	Tail  bool   `json:tail`
}

type Storage interface {
	HealthCheck() error
	Init(url string) error
	SetSession(key string, value LogSession, duration time.Duration) error
	GetSession(key string) (LogSession, error)
	GetRoutes() ([]Route, error)
	GetRouteById(id string) (*Route, error)
	RemoveRoute(route Route) error
	AddRoute(route Route) error
	AddRoutes([]Route) error
	Close()
}

func MarshalRoute(route Route) (string, error) {
	bytes, err := json.Marshal(route)
	if err != nil {
		return "", nil
	}
	return string(bytes), nil
}

func UnmarshalRoute(route string) (Route, error) {
	var r Route
	if err := json.Unmarshal([]byte(route), &r); err != nil {
		return r, err
	}
	return r, nil
}

// Postgres Interface

type PostgresStorage struct {
	Storage
	client *sql.DB
	open bool
}

func (rs *PostgresStorage) HealthCheck() error {
	var val bool
	err := rs.client.QueryRow("select true").Scan(&val)
	if err != nil {
		return err
	}
	if val != true {
		return errors.New("Something bizarre happened.")
	}
	return err
}

func (rs *PostgresStorage) Init(url string) error {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return err
	}
	_, err = db.Exec("create table if not exists drains (drain varchar(128) not null primary key, app text not null, space text not null, created timestamptz, updated timestamptz, destination text not null)")
	if err != nil {
		return err
	}
	_, err = db.Exec("create table if not exists sessions (session varchar(128) not null primary key, app text not null, space text not null, lines int, tail boolean, expiration timestamptz default now())")
	if err != nil {
		return err
	}
	rs.open = true
	rs.client = db

	t := time.NewTicker(time.Second * 60)
	go func() {
		for rs.open == true {
			rs.client.Exec("delete from sessions where expiration < now()")
			<-t.C
		}
	}()
	return nil
}

func (rs *PostgresStorage) Close() {
	if rs.open == true {
		rs.client.Close()
	}
	rs.open = false
}

func (rs *PostgresStorage) SetSession(key string, value LogSession, duration time.Duration) error {
	_, err := rs.client.Exec("insert into sessions(session, app, space, lines, tail, expiration) values ($1, $2, $3, $4, $5, $6)",
		key, value.App, value.Space, value.Lines, value.Tail, time.Now().Add(duration))
	return err
}

func (rs *PostgresStorage) GetSession(key string) (value LogSession, err error) {
	err = rs.client.QueryRow("select session, app, space, lines, tail from sessions where session=$1 and expiration >= now()", key).Scan(&key, &value.App, &value.Space, &value.Lines, &value.Tail)
	return value, err
}

func (rs *PostgresStorage) GetRoutes() ([]Route, error) {
	rows, err := rs.client.Query("select drain, app, space, created, updated, destination from drains")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []Route = make([]Route, 0)
	for rows.Next() {
		var route Route
		err = rows.Scan(&route.Id, &route.App, &route.Space, &route.Created, &route.Updated, &route.DestinationUrl)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}
	return routes, nil
}

func (rs *PostgresStorage) GetRouteById(Id string) (*Route, error) {
	var route Route
	err := rs.client.QueryRow("select drain, app, space, created, updated, destination from drains where drain=$1", Id).Scan(&route.Id, &route.App, &route.Space, &route.Created, &route.Updated, &route.DestinationUrl)
	return &route, err
}

func (rs *PostgresStorage) RemoveRoute(route Route) error {
	_, err := rs.client.Exec("delete from drains where drain=$1", route.Id)
	return err
}

func (rs *PostgresStorage) AddRoute(route Route) error {
	_, err := rs.client.Exec("insert into drains (drain, app, space, created, updated, destination) values ($1, $2, $3, $4, $5, $6) on conflict do nothing", route.Id, route.App, route.Space, route.Created, route.Updated, route.DestinationUrl)
	return err
}

func (rs *PostgresStorage) AddRoutes(routes []Route) (err error) {
	for _, route := range routes {
		err = rs.AddRoute(route)
		if err != nil {
			return err
		}
	}
	return nil
}



// Redis Interface

type RedisStorage struct {
	Storage
	client *redis.Client
}

func (rs *RedisStorage) HealthCheck() error {
	_, err := rs.client.Info("all").Result()
	return err
}

func (rs *RedisStorage) Init(url string) error {
	rs.client = redis.NewClient(&redis.Options{
		Addr:     url,
		Password: "",
		DB:       0,
	})
	return nil
}

func (rs *RedisStorage) SetSession(key string, value LogSession, duration time.Duration) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = rs.client.Set(key, string(bytes), duration).Result()
	return err
}

func (rs *RedisStorage) GetSession(key string) (value LogSession, err error) {
	bytes, err := rs.client.Get(key).Result()
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(bytes), &value); err != nil {
		return value, err
	}
	return value, nil
}

func (rs *RedisStorage) GetRoutes() ([]Route, error) {
	var routes []Route
	vals, err := rs.client.LRange("routes", 0, -1).Result()
	if err != nil {
		return nil, err
	}
	for _, val := range vals {
		route, err := UnmarshalRoute(string(val))
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (rs *RedisStorage) GetRouteById(Id string) (*Route, error) {
	routes_pkg, err := rs.GetRoutes()
	if err != nil {
		return nil, err
	}
	for _, r := range routes_pkg {
		if r.Id == Id {
			return &r, nil
		}
	}
	return nil, errors.New("No such drain found.")
}

func (rs *RedisStorage) RemoveRoute(route Route) error {
	b, err := MarshalRoute(route)
	if err != nil {
		return err
	}
	_, err = rs.client.LRem("routes", 1, b).Result()
	return err
}

func (rs *RedisStorage) AddRoute(route Route) error {
	b, err := MarshalRoute(route)
	if err != nil {
		return err
	}
	_, err = rs.client.RPush("routes", b).Result()
	return err
}

func (rs *RedisStorage) AddRoutes(routes []Route) error {
	for _, route := range routes {
		b, err := MarshalRoute(route)
		if err != nil {
			return err
		}
		_, err = rs.client.RPush("routes", b).Result()
		if err != nil {
			return err
		}
	}
	return nil
}



// Memory Interface

type MemoryStorage struct {
	Storage
	routes []Route
	sessions map[string]LogSession
}

func (ms *MemoryStorage) HealthCheck() error {
	return nil
}

// Url is just a dummy interface.
func (ms *MemoryStorage) Init(url string) error {
	ms.routes = make([]Route, 0)
	ms.sessions = make(map[string]LogSession)
	return nil
}

func (ms *MemoryStorage) SetSession(key string, value LogSession, duration time.Duration) error {
	ms.sessions[key] = value
	go func() {
		t := time.NewTicker(duration)
		<-t.C
		delete(ms.sessions, key)
	}()
	return nil
}

func (ms *MemoryStorage) GetSession(key string) (value LogSession, err error) {
	val, ok := ms.sessions[key]
	if ok == true {
		return val, nil
	} else {
		return LogSession{}, errors.New("Element was not found.")
	}
}

func (ms *MemoryStorage) GetRoutes() ([]Route, error) {
	return ms.routes, nil
}

func (ms *MemoryStorage) GetRouteById(Id string) (*Route, error) {
	routes_pkg, err := ms.GetRoutes()
	if err != nil {
		return nil, err
	}
	for _, r := range routes_pkg {
		if r.Id == Id {
			return &r, nil
		}
	}
	return nil, errors.New("No such drain found.")
}

func (ms *MemoryStorage) RemoveRoute(route Route) error {
	routes_pkg, _ := ms.GetRoutes()
	for i, r := range routes_pkg {
		if r.Id == route.Id {
			ms.routes = append(ms.routes[:i], ms.routes[i+1:]...)
			break;
		}
	}
	return nil
}

func (ms *MemoryStorage) AddRoute(route Route) error {
	ms.routes = append(ms.routes, route)
	return nil
}

func (ms *MemoryStorage) AddRoutes(routes []Route) error {
	ms.routes = append(ms.routes, routes...)
	return nil
}
