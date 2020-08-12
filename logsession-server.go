package main

import (
	"github.com/akkeris/logshuttle/shuttle"
	"github.com/akkeris/logshuttle/storage"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

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
		r.JSON(http.StatusCreated, map[string]interface{}{"id": id.String(), "logplex_url": os.Getenv("SESSION_URL") + "/log-sessions/" + id.String()})
	}
}

func ReadLogSession(client *storage.Storage, kafkaAddrs []string) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		logSess, err := (*client).GetSession(params["id"])
		if err != nil {
			log.Printf("Cannot find id %s\n", params["id"])
			res.WriteHeader(http.StatusNotFound)
			return
		}
		res.WriteHeader(http.StatusOK)
		var ls shuttle.Session
		ls.ConsumeAndRespond(kafkaAddrs, logSess.App, logSess.Space, logSess.Site, req, res)
	}
}

func StartSessionServices(client *storage.Storage, kafkaAddrs []string, port int) {
	log.Println("[info] Starting logsession...")
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
