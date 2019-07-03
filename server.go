package main

import (
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"github.com/stackimpact/stackimpact-go"
	"log"
	"github.com/akkeris/logshuttle/drains"
	"github.com/akkeris/logshuttle/events"
	"github.com/akkeris/logshuttle/shuttle"
	"github.com/akkeris/logshuttle/storage"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type logDrainCreateRequest struct {
	Url string `json:"url"`
}

type addonResponse struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type logDrainResponse struct {
	Addon     addonResponse `json:"addon"`
	CreatedAt time.Time     `json:"created_at"`
	Id        string        `json:"id"`
	Token     string        `json:"token"`
	UpdatedAt time.Time     `json:"updated_at"`
	Url       string        `json:"url"`
}

func ReportInvalidRequest(r render.Render) {
	r.JSON(400, "Malformed Request")
}

func ReportError(r render.Render, err error) {
	log.Printf("error: %s", err)
	r.JSON(500, map[string]interface{}{"message": "Internal Server Error"})
}

func Filter(vs []string, f func(string) bool) []string {
	vsf := make([]string, 0)
	for _, v := range vs {
		if f(v) {
			vsf = append(vsf, v)
		}
	}
	return vsf
}

func HealthCheck(client *storage.Storage) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		err := (*client).HealthCheck()
		if err != nil {
			log.Printf("error: %s", err)
			res.WriteHeader(200)
			res.Write([]byte("overall_status=bad,redis_check=failed"))
		} else {
			res.WriteHeader(200)
			res.Write([]byte("overall_status=good"))
		}
	}
}

func ListLogDrains(client *storage.Storage, isSite bool) func(martini.Params, render.Render) {
	return func(params martini.Params, rr render.Render) {
		if params["key"] == "" {
			ReportInvalidRequest(rr)
			return
		}

		routes_pkg, err := (*client).GetRoutes()
		if err != nil {
			ReportError(rr, err)
			return
		}

		var resp = make([]logDrainResponse, 0)
		if !isSite {
			var app_keys = strings.SplitN(params["key"], "-", 2)
			var app = app_keys[0]
			var space = app_keys[1]
			if app == "" || space == "" {
				ReportInvalidRequest(rr)
				return
			}
			for _, r := range routes_pkg {
				if r.App == app && r.Space == space {
					var n = logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: r.Created, UpdatedAt: r.Updated, Id: r.Id, Token: app + "-" + space, Url: r.DestinationUrl}
					resp = append(resp, n)
				}
			}
		} else {
			for _, r := range routes_pkg {
				if params["key"] == r.Site {
					var n = logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: r.Created, UpdatedAt: r.Updated, Id: r.Id, Token: r.Site, Url: r.DestinationUrl}
					resp = append(resp, n)
				}
			}
		}
		rr.JSON(200, resp)
	}
}

func CreateLogEvent(kafkaProducer events.LogProducer) func(martini.Params, events.LogSpec, binding.Errors, render.Render) {
	return func(params martini.Params, opts events.LogSpec, berr binding.Errors, r render.Render) {
		if berr != nil {
			ReportInvalidRequest(r)
			return
		}
		err := kafkaProducer.AddLog(opts)
		if err != nil {
			ReportError(r, err)
			return
		}
		r.JSON(201, opts)
	}
}

func CreateLogDrain(client *storage.Storage, isSite bool) func(martini.Params, logDrainCreateRequest, binding.Errors, render.Render) {
	return func(params martini.Params, opts logDrainCreateRequest, berr binding.Errors, r render.Render) {
		if berr != nil {
			ReportInvalidRequest(r)
			return
		}
		if params["key"] == "" {
			ReportInvalidRequest(r)
			return
		}
		id, err := uuid.NewV4()
		if err != nil {
			ReportError(r, err)
			return
		}
		if isSite {
			err = (*client).AddRoute(storage.Route{Id: id.String(), Site: params["key"], Space: "", App: "", DestinationUrl: opts.Url, Created: time.Now(), Updated: time.Now()})
		} else {
			var app_keys = strings.SplitN(params["key"], "-", 2)
			var app = app_keys[0]
			var space = app_keys[1]
			if app == "" || space == "" {
				ReportInvalidRequest(r)
				return
			}
			err = (*client).AddRoute(storage.Route{Id: id.String(), Space: space, App: app, DestinationUrl: opts.Url, Created: time.Now(), Updated: time.Now()})
		}
		if err != nil {
			ReportError(r, err)
			return
		}
		r.JSON(201, logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: time.Now(), UpdatedAt: time.Now(), Id: id.String(), Token: params["key"], Url: opts.Url})
	}
}

func DeleteLogDrain(client *storage.Storage, isSite bool) func(martini.Params, render.Render) {
	return func(params martini.Params, r render.Render) {
		if params["key"] == "" {
			ReportInvalidRequest(r)
			return
		}
		var route, err = (*client).GetRouteById(params["id"])
		err = (*client).RemoveRoute(*route)
		if err != nil {
			ReportError(r, err)
			return
		}

		r.JSON(200, logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: params["key"], Url: route.DestinationUrl})
	}
}

func GetLogDrain(client *storage.Storage, isSite bool) func(martini.Params, render.Render) {
	return func(params martini.Params, r render.Render) {
		if params["key"] == "" {
			ReportInvalidRequest(r)
			return
		}

		var route, err = (*client).GetRouteById(params["id"])
		if err != nil {
			r.JSON(404, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		if params["key"] != route.Site && params["key"] != (route.App+"-"+route.Space) {
			r.JSON(404, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		r.JSON(200, logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: params["key"], Url: route.DestinationUrl})
	}
}

func StartHttpShuttleServices(client *storage.Storage, producer events.LogProducer, port int) {
	m := martini.Classic()
	m.Use(func(res http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != os.Getenv("AUTH_KEY") && req.URL.Path != "/octhc" {
			res.WriteHeader(http.StatusUnauthorized)
		}
	})
	m.Use(render.Renderer())
	m.Get("/apps/:key/log-drains", ListLogDrains(client, false))
	m.Post("/apps/:key/log-drains", binding.Json(logDrainCreateRequest{}), CreateLogDrain(client, false))
	m.Delete("/apps/:key/log-drains/:id", DeleteLogDrain(client, false))
	m.Get("/apps/:key/log-drains/:id", GetLogDrain(client, false))

	m.Get("/sites/:key/log-drains", ListLogDrains(client, true))
	m.Post("/sites/:key/log-drains", binding.Json(logDrainCreateRequest{}), CreateLogDrain(client, true))
	m.Delete("/sites/:key/log-drains/:id", DeleteLogDrain(client, true))
	m.Get("/sites/:key/log-drains/:id", GetLogDrain(client, true))

	m.Get("/octhc", HealthCheck(client))
	// Private end point to create new events within the log stream that are controller-api specifc.
	m.Post("/log-events", binding.Json(events.LogSpec{}), CreateLogEvent(producer))
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}

func CreateLogSession(client *storage.Storage) func(martini.Params, storage.LogSession, binding.Errors, render.Render) {
	return func(params martini.Params, logSess storage.LogSession, berr binding.Errors, r render.Render) {
		if berr != nil {
			ReportInvalidRequest(r)
			return
		}
		id, err := uuid.NewV4()
		if err != nil {
			ReportError(r, err)
			return
		}
		err = (*client).SetSession(id.String(), logSess, time.Minute*5)
		if err != nil {
			ReportError(r, err)
			return
		}
		r.JSON(201, map[string]interface{}{"id": id.String(), "logplex_url": os.Getenv("SESSION_URL") + "/log-sessions/" + id.String()})
	}
}

func ReadLogSession(client *storage.Storage, kafkaAddrs []string) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		logSess, err := (*client).GetSession(params["id"])
		if err != nil {
			log.Printf("Cannot find id %s\n", params["id"])
			res.WriteHeader(404)
			return
		}
		res.WriteHeader(200)
		var ls shuttle.Session
		ls.ConsumeAndRespond(kafkaAddrs, logSess.App, logSess.Space, logSess.Site, res)
	}
}

func StartShuttleServices(client *storage.Storage, kafkaAddrs []string, port int, kafkaGroup string) {
	var logProducer events.LogProducer
	logProducer.Init(kafkaAddrs, kafkaGroup)

	// Load routes
	drains.Init()

	var logShuttle shuttle.Shuttle
	logShuttle.Init(client, kafkaAddrs, kafkaGroup)
	if os.Getenv("TEST_MODE") != "" {
		logShuttle.EnableTestMode()
	}

	// Start http services
	go StartHttpShuttleServices(client, logProducer, port)
	t := time.NewTicker(time.Second * 60)

	// we need to hear about interrupt signals to safely
	// close the kafka channel, flush syslogs, etc..
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigchan
		t.Stop()
		log.Println("[info] Shutting down, timer stopped.")
		logProducer.Close()
		log.Println("[info] Closed producer.")
		logShuttle.Close()
		log.Println("[info] Closed consumer.")
		drains.CloseAll()
		log.Println("[info] Closed syslog drains.")
		os.Exit(0)
	}()
	for {
		drains.PrintMetrics()
		logShuttle.PrintMetrics()
		logShuttle.Refresh()
		<-t.C
	}
}

func StartSessionServices(client *storage.Storage, kafkaAddrs []string, port int) {
	m := martini.Classic()
	m.Use(func(res http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" && req.URL.Path == "/log-sessions" && req.Header.Get("Authorization") != os.Getenv("AUTH_KEY") {
			res.WriteHeader(http.StatusUnauthorized)
		}
	})
	m.Use(render.Renderer())
	// IMPORTANT: Only POST /log-sessions is protected
	m.Post("/log-sessions", binding.Json(storage.LogSession{}), CreateLogSession(client))
	m.Get("/log-sessions/:id", ReadLogSession(client, kafkaAddrs))
	m.Get("/octhc", HealthCheck(client))
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}

func main() {
	var kafkaGroup = "logshuttle"
	// Get kafka group for testing.
	if os.Getenv("TEST_MODE") != "" {
		log.Printf("Using kafka group logshuttle-testing for testing purposes...\n")
		kafkaGroup = "logshuttletest"
	} else {
		// Purposely wait a random amount of time to allow
		// kafka to more easily balance more than one logshuttle, if the
		// connection between kafka is too close, partition assignment
		// can sometimes take a very long time. Seems odd, but helps.
		time.Sleep(time.Duration(rand.Intn(30)) * time.Second)
	}
	if os.Getenv("STACKIMPACT") != "" {
		stackimpact.Start(stackimpact.Options{
			AgentKey: os.Getenv("STACKIMPACT"),
			AppName:  "Logshuttle",
		})
	}

	// Connect to storage instance
	var s storage.Storage
	if os.Getenv("REDIS_URL") != "" {
		var redis storage.RedisStorage
		if err := redis.Init(strings.Replace(os.Getenv("REDIS_URL"), "redis://", "", 1)); err != nil {
			log.Fatalf("Fatal: Cannot connect to redis: %v\n", err)
		}
		s = &redis
	} else if os.Getenv("POSTGRES_URL") != "" {
		var postgres storage.PostgresStorage
		if err := postgres.Init(os.Getenv("POSTGRES_URL")); err != nil {
			log.Fatalf("Fatal: Cannot connect to postgres: %v\n", err)
		}
		s = &postgres
	} else {
		log.Fatalf("Cannot find REDIS_URL or POSTGRES_URL. Abandoning ship.\n")
	}

	kafkaAddrs := strings.Split(os.Getenv("KAFKA_HOSTS"), ",")
	port, err := strconv.Atoi(os.Getenv("PORT"))

	if err != nil {
		port = 5000
	}

	if os.Getenv("PROFILE") != "" {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
			http.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
			http.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
			http.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
			http.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
			http.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
		}()
	}

	if os.Getenv("RUN_SESSION") != "" {
		StartSessionServices(&s, kafkaAddrs, port)
	} else {
		StartShuttleServices(&s, kafkaAddrs, port, kafkaGroup)
	}
}
