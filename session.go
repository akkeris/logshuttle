package main

import (
	"log"
	"os"
	"strings"
	"net/http"
	"encoding/json"
	"time"
	"gopkg.in/redis.v4"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"strconv"
	"fmt"
)

func ConsumeAndRespond(kafkaAddrs []string, app string, space string, listenspace []string, res http.ResponseWriter, group string) {
	cluster := CreateConsumerCluster(kafkaAddrs, group, listenspace)
	var last_date = time.Now()
	defer cluster.Consumer.Close()
	for {
		select {
			case message := <-cluster.Consumer.Messages():
				if message.Topic == space {
					var msg LogSpec
					if err := json.Unmarshal(message.Value, &msg); err == nil {
						if IsAppMatch(msg.Kubernetes.ContainerName, app) && msg.Topic == space {
							if msg.Time.Unix() < last_date.Unix() {
								msg.Time = time.Now()
							}
							last_date = msg.Time
							proc := Process{App: msg.Kubernetes.ContainerName, Type: "web"}
							if strings.Index(msg.Kubernetes.ContainerName, "--") != -1 {
								var components = strings.SplitN(msg.Kubernetes.ContainerName, "--", 2)
								proc = Process{App: components[0], Type: components[1]}
							}
							_, err := res.Write([]byte(msg.Time.UTC().Format(time.RFC3339) + " " + app + "-" + space + " app[" + proc.Type + "." + strings.Replace(strings.Replace(msg.Kubernetes.PodName, "-"+proc.Type+"-", "", 1), proc.App+"-", "", 1) + "]: " + strings.TrimSpace(KubernetesToHumanReadable(msg.Log)) + "\n"))
							if err != nil {
								return
							} else if f, ok := res.(http.Flusher); ok {
								f.Flush()
							}
						}
					}
				} else if message.Topic == "alamoweblogs" {
					var msg LogSpec
					err := ParseWebLogMessage(message.Value, &msg)
					if err == false && IsAppMatch(msg.Kubernetes.ContainerName, app) && msg.Topic == space {
						if msg.Time.Unix() < last_date.Unix() {
							msg.Time = time.Now()
						}
						last_date = msg.Time
						_, err2 := res.Write([]byte(msg.Time.UTC().Format(time.RFC3339) + " " + app + "-" + space + " alamo/router: " + msg.Log + "\n"))
						if err2 != nil {
							return
						} else if f, ok := res.(http.Flusher); ok {
							f.Flush()
						}
					} 
				} else if message.Topic == "alamobuildlogs" {
					var bmsg BuildLogSpec
					if err := json.Unmarshal(message.Value, &bmsg); err == nil {
						logmsg, errd := ParseBuildLogMessage(bmsg)
						if errd == false && IsAppMatch(logmsg.Kubernetes.ContainerName, app) && logmsg.Topic == space {
							if logmsg.Time.Unix() < last_date.Unix() {
								logmsg.Time = time.Now()
							}
							last_date = logmsg.Time
							_, err2 := res.Write([]byte(logmsg.Time.UTC().Format(time.RFC3339) + " " + app + "-" + space + " akkeris/build: " + logmsg.Log + "\n"))
							if err2 != nil {
								return
							} else if f, ok := res.(http.Flusher); ok {
								f.Flush()
							}
						}
					} else {
						fmt.Println("ERROR: Failed to parse build log")
					}
				} 
				cluster.Consumer.MarkOffset(message, "")
		}
	}
}

func CreateLogSession(client *redis.Client) func(martini.Params, LogSession, binding.Errors, render.Render) {
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
		bytes, err := json.Marshal(logSess)
		if err != nil {
			ReportError(r, err)
			return
		}
		s, err := client.Set(id.String(), bytes, time.Minute*5).Result()
		if err != nil {
			ReportError(r, err)
			return
		}
		log.Printf(s)
		r.JSON(201, map[string]interface{}{"id": id.String()})
	}
}

func ReadLogSession(client *redis.Client, kafkaAddrs []string) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		logSessionString, err := client.Get(params["id"]).Result()
		if err != nil {
			DumpToSyslog("Cannot find id %s", params["id"])
			res.WriteHeader(404)
			return
		}
		var logSess LogSession
		if err := json.Unmarshal([]byte(logSessionString), &logSess); err != nil {
			res.WriteHeader(500)
			return
		}
		res.WriteHeader(200)

		ConsumeAndRespond(kafkaAddrs, logSess.App, logSess.Space, []string{logSess.Space, "alamoweblogs", "alamobuildlogs"}, res, RandomString(16))
	}
}

func StartSessionServices(client *redis.Client, kafkaAddrs []string, port int) {
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
	m.Get("/octhc", HealthCheck(client, kafkaAddrs))
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}
