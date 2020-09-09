package main

import (
	"github.com/akkeris/logshuttle/drains"
	"github.com/akkeris/logshuttle/events"
	"github.com/akkeris/logshuttle/shuttle"
	"github.com/akkeris/logshuttle/storage"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"net/http/pprof"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type addonResponse struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type logDrainCreateRequest struct {
	Url string `json:"url"`
}

type logDrainResponse struct {
	Addon     addonResponse `json:"addon"`
	CreatedAt time.Time     `json:"created_at"`
	Id        string        `json:"id"`
	Token     string        `json:"token"`
	UpdatedAt time.Time     `json:"updated_at"`
	Url       string        `json:"url"`
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

		r.JSON(http.StatusOK, logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: params["key"], Url: route.DestinationUrl})
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
			r.JSON(http.StatusNotFound, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		if params["key"] != route.Site && params["key"] != (route.App+"-"+route.Space) {
			r.JSON(http.StatusNotFound, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		r.JSON(http.StatusOK, logDrainResponse{Addon: addonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: params["key"], Url: route.DestinationUrl})
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
		rr.JSON(http.StatusOK, resp)
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

	var envoyAlsAdapter *shuttle.EnvoyAlsServer = &shuttle.EnvoyAlsServer{}
	if os.Getenv("RUN_ISTIO_ALS") == "true" {
		go envoyAlsAdapter.StartEnvoyALSAdapter(9001, logProducer)
	}

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
		if os.Getenv("RUN_ISTIO_ALS") == "true" {
			envoyAlsAdapter.Close()
			log.Println("[info] Closed envoy als adapter.")
		}
		os.Exit(0)
	}()
	for {
		drains.PrintMetrics()
		logShuttle.PrintMetrics()
		logShuttle.Refresh()
		<-t.C
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
		r.JSON(http.StatusCreated, opts)
	}
}

func StartHttpShuttleServices(client *storage.Storage, producer events.LogProducer, port int) {
	log.Println("[info] Starting logshuttle...")
	m := martini.Classic()
	m.Use(func(res http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != os.Getenv("AUTH_KEY") && req.URL.Path != "/octhc" && !strings.Contains(req.URL.Path, "/debug") {
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

	if os.Getenv("PROFILE") != "" {
		m.Group("/debug/pprof", func(r martini.Router) {
			r.Any("/", http.HandlerFunc(pprof.Index))
			r.Any("/cmdline", http.HandlerFunc(pprof.Cmdline))
			r.Any("/profile", http.HandlerFunc(pprof.Profile))
			r.Any("/symbol", http.HandlerFunc(pprof.Symbol))
			r.Any("/trace", http.HandlerFunc(pprof.Trace))
			r.Any("/heap", pprof.Handler("heap").ServeHTTP)
			r.Any("/block", pprof.Handler("block").ServeHTTP)
			r.Any("/goroutine", pprof.Handler("goroutine").ServeHTTP)
			r.Any("/mutex", pprof.Handler("mutex").ServeHTTP)
		})
	}
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}
