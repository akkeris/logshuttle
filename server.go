package main

import (
	"./drains"
	"fmt"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"github.com/stackimpact/stackimpact-go"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func ListLogDrains(client *Storage) func(martini.Params, render.Render) {
	return func(params martini.Params, rr render.Render) {
		if params["app_key"] == "" {
			ReportInvalidRequest(rr)
			return
		}

		var app_keys = strings.SplitN(params["app_key"], "-", 2)
		var app = app_keys[0]
		var space = app_keys[1]
		if app == "" || space == "" {
			ReportInvalidRequest(rr)
			return
		}

		routes_pkg, err := (*client).GetRoutes()
		if err != nil {
			ReportError(rr, err)
			return
		}

		var resp = make([]LogDrainResponse, 0)
		for _, r := range routes_pkg {
			if r.App == app && r.Space == space {
				var n = LogDrainResponse{Addon: AddonResponse{Id: "", Name: ""}, CreatedAt: r.Created, UpdatedAt: r.Updated, Id: r.Id, Token: app + "-" + space, Url: r.DestinationUrl}
				resp = append(resp, n)
			}
		}
		rr.JSON(200, resp)
	}
}

func CreateLogEvent(kafkaProducer LogProducer) func(martini.Params, LogSpec, binding.Errors, render.Render) {
	return func(params martini.Params, opts LogSpec, berr binding.Errors, r render.Render) {
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

func CreateLogDrain(client *Storage) func(martini.Params, LogDrainCreateRequest, binding.Errors, render.Render) {
	return func(params martini.Params, opts LogDrainCreateRequest, berr binding.Errors, r render.Render) {
		if berr != nil {
			ReportInvalidRequest(r)
			return
		}
		if params["app_key"] == "" {
			ReportInvalidRequest(r)
			return
		}
		var app_keys = strings.SplitN(params["app_key"], "-", 2)
		var app = app_keys[0]
		var space = app_keys[1]
		if app == "" || space == "" {
			ReportInvalidRequest(r)
			return
		}
		id, err := uuid.NewV4()
		if err != nil {
			ReportError(r, err)
			return
		}
		err = (*client).AddRoute(Route{Id: id.String(), Space: space, App: app, DestinationUrl: opts.Url, Created: time.Now(), Updated: time.Now()})
		if err != nil {
			ReportError(r, err)
			return
		}
		r.JSON(201, LogDrainResponse{Addon: AddonResponse{Id: "", Name: ""}, CreatedAt: time.Now(), UpdatedAt: time.Now(), Id: id.String(), Token: app + "-" + space, Url: opts.Url})
	}
}

func DeleteLogDrain(client *Storage) func(martini.Params, render.Render) {
	return func(params martini.Params, r render.Render) {
		if params["app_key"] == "" {
			ReportInvalidRequest(r)
			return
		}
		var app_keys = strings.SplitN(params["app_key"], "-", 2)
		var app = app_keys[0]
		var space = app_keys[1]
		if app == "" || space == "" || params["id"] == "" {
			ReportInvalidRequest(r)
			return
		}
		var route, err = (*client).GetRouteById(params["id"])
		err = (*client).RemoveRoute(*route)
		if err != nil {
			ReportError(r, err)
			return
		}

		r.JSON(200, LogDrainResponse{Addon: AddonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: app + "-" + space, Url: route.DestinationUrl})
	}
}

func GetLogDrain(client *Storage) func(martini.Params, render.Render) {
	return func(params martini.Params, r render.Render) {
		if params["app_key"] == "" {
			ReportInvalidRequest(r)
			return
		}
		var app_keys = strings.SplitN(params["app_key"], "-", 2)
		var app = app_keys[0]
		var space = app_keys[1]
		if app == "" || space == "" || params["id"] == "" {
			ReportInvalidRequest(r)
			return
		}
		var route, err = (*client).GetRouteById(params["id"])
		if err != nil {
			r.JSON(404, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		r.JSON(200, LogDrainResponse{Addon: AddonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: app + "-" + space, Url: route.DestinationUrl})
	}
}

func StartHttpShuttleServices(client *Storage, producer LogProducer, port int) {
	m := martini.Classic()
	m.Use(func(res http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != os.Getenv("AUTH_KEY") && req.URL.Path != "/octhc" {
			res.WriteHeader(http.StatusUnauthorized)
		}
	})
	m.Use(render.Renderer())
	m.Get("/apps/:app_key/log-drains", ListLogDrains(client))
	m.Post("/apps/:app_key/log-drains", binding.Json(LogDrainCreateRequest{}), CreateLogDrain(client))
	m.Delete("/apps/:app_key/log-drains/:id", DeleteLogDrain(client))
	m.Get("/apps/:app_key/log-drains/:id", GetLogDrain(client))
	m.Get("/octhc", HealthCheck(client))
	// Private end point to create new events within the log stream that are controller-api specifc.
	m.Post("/log-events", binding.Json(LogSpec{}), CreateLogEvent(producer))
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}

func CreateLogSession(client *Storage) func(martini.Params, LogSession, binding.Errors, render.Render) {
	return func(params martini.Params, logSess LogSession, berr binding.Errors, r render.Render) {
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
		r.JSON(201, map[string]interface{}{"id": id.String()})
	}
}

func ReadLogSession(client *Storage, kafkaAddrs []string) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		logSess, err := (*client).GetSession(params["id"])
		if err != nil {
			fmt.Printf("Cannot find id %s\n", params["id"])
			res.WriteHeader(404)
			return
		}
		res.WriteHeader(200)
		var ls Session
		ls.ConsumeAndRespond(kafkaAddrs, logSess.App, logSess.Space, res)
	}
}

func StartShuttleServices(client *Storage, kafkaAddrs []string, port int, kafkaGroup string) {

	var logProducer LogProducer
	logProducer.Init(kafkaAddrs, kafkaGroup)

	// Load routes
	drains.InitSyslogDrains()
	drains.InitUrlDrains()

	var shuttle Shuttle
	shuttle.Init(client, kafkaAddrs, kafkaGroup)
	if os.Getenv("TEST_MODE") != "" {
		shuttle.EnableTestMode()
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
		fmt.Println("[info] Shutting down, timer stopped.")
		drains.CloseSyslogDrains()
		fmt.Println("[info] Closed syslog drains.")
		logProducer.Close()
		fmt.Println("[info] Closed producer.")
		shuttle.Close()
		fmt.Println("[info] Closed consumer.")
		os.Exit(0)
	}()
	for {
		drains.PrintMetrics()
		shuttle.PrintMetrics()
		shuttle.Refresh()
		<-t.C
	}
}

func StartSessionServices(client *Storage, kafkaAddrs []string, port int) {
	m := martini.Classic()
	m.Use(func(res http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" && req.URL.Path == "/log-sessions" && req.Header.Get("Authorization") != os.Getenv("AUTH_KEY") {
			res.WriteHeader(http.StatusUnauthorized)
		}
	})
	m.Use(render.Renderer())
	// IMPORTANT: Only POST /log-sessions is protected
	m.Post("/log-sessions", binding.Json(LogSession{}), CreateLogSession(client))
	m.Get("/log-sessions/:id", ReadLogSession(client, kafkaAddrs))
	m.Get("/octhc", HealthCheck(client))
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}

func main() {
	var kafkaGroup = "logshuttle"
	// Get kafka group for testing.
	if os.Getenv("TEST_MODE") != "" {
		fmt.Printf("Using kafka group logshuttle-testing for testing purposes...\n")
		kafkaGroup = "logshuttletest"
	}
	if os.Getenv("STACKIMPACT") != "" {
		stackimpact.Start(stackimpact.Options{
			AgentKey: os.Getenv("STACKIMPACT"),
			AppName:  "Logshuttle",
		})
	}

	// Connect to storage (usually redis) instance
	var storage RedisStorage
	storage.Init(strings.Replace(os.Getenv("REDIS_URL"), "redis://", "", 1))
	var s Storage = &storage

	kafkaAddrs := strings.Split(os.Getenv("KAFKA_HOSTS"), ",")
	port, err := strconv.Atoi(os.Getenv("PORT"))

	if err != nil {
		port = 5000
	}

	if os.Getenv("PROFILE") != "" {
		go func() {
			fmt.Println(http.ListenAndServe("localhost:6060", nil))
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
