package main

import (
	"./drains"
	"errors"
	"encoding/json"
	"log"
	"fmt"
	"os"
	"strings"
	"time"
	"net/http"
	"./syslog"
	"github.com/trevorlinton/sarama"
	"github.com/bsm/sarama-cluster"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"strconv"
	"sync"
	"runtime"
)

// TODO: Support tokens
// TODO: Connect on demand (but deal with bad hosts will be tricky)
// TODO: Mark in stoage errors connecting

var messagesSent = 0
var messagesReceived = 0
var messageFailedDecode = 0

// Max syslog length is 100kb, we'll be a bit more conservative and accept 99kb.
var testMode = false
var routes map[string][]Route
var routex = &sync.Mutex{}

func AddLogsToApp(producer sarama.AsyncProducer, message LogSpec) error {
	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}
	msg := &sarama.ProducerMessage{Topic: message.Topic, Key: sarama.ByteEncoder("message"), Value: sarama.ByteEncoder(bytes)}
	producer.Input() <- msg
	return nil
}

func SendMessage(message LogSpec) {
	proc := Process{App: message.Kubernetes.ContainerName, Type: "web"}

	if strings.Index(message.Kubernetes.ContainerName, "--") != -1 {
		var components = strings.SplitN(message.Kubernetes.ContainerName, "--", 2)
		proc = Process{App: components[0], Type: components[1]}
	}

	routex.Lock()
	r := routes[proc.App+message.Topic]
	routex.Unlock()

	for _, route := range r {
		tag := proc.Type + "." + strings.Replace(strings.Replace(message.Kubernetes.PodName, "-"+proc.Type+"-", "", 1), proc.App+"-", "", 1)
		if strings.HasPrefix(message.Kubernetes.PodName, "akkeris/") {
			tag = message.Kubernetes.PodName
		}
		var host = proc.App + "-" + message.Topic
		if testMode {
			host = "logshuttle-test"
		}
		var p = syslog.Packet{
			Severity: syslog.SevInfo,
			Facility: syslog.LogUser,
			Hostname: host, 
			Tag:      tag,
			Time:     message.Time,
			Message:  KubernetesToHumanReadable(message.Log),
		}
		route.Destination.Packets <- p
		messagesSent++
	}
}

// Works for every topic other than __consumer_offsets, alamobuildlogs and alamoweblogs.
func StartForwardingAppLogs(consumer *cluster.Consumer) {
	for {
		select {
		case message := <-consumer.Messages():
			var msg LogSpec
			messagesReceived++
			if err := json.Unmarshal(message.Value, &msg); err != nil {
				messageFailedDecode++
			} else {
				SendMessage(msg)
			}
			consumer.MarkOffset(message, "")
		}
	}
}

// Only works for alamoweblogs.
func StartForwardingWebLogs(consumer *cluster.Consumer) {
	for {
		select {
		case message := <-consumer.Messages():
			var msg LogSpec
			messagesReceived++
			if err := ParseWebLogMessage(message.Value, &msg); err == true {
				messageFailedDecode++
			} else {
				SendMessage(msg)
			}
			consumer.MarkOffset(message, "")
		}
	}
}

// Only works for alamobuildlogs.
func StartForwardingBuildLogs(consumer *cluster.Consumer) {
	for {
		select {
		case message := <-consumer.Messages():
			var msg LogSpec
			messagesReceived++
			if err := ParseBuildLogMessage(message.Value, &msg); err == true {
				messageFailedDecode++
			} else {
				SendMessage(msg)
			}
			consumer.MarkOffset(message, "")
		}
	}
}

func RefreshRoutes(client *Storage, kafkaGroup string) {
	routesPkg, err := (*client).GetRoutes()
	if err != nil {
		log.Fatalf("[shuttle] error: cannot obtain routes: %s", err)
		return
	}

	wg := new(sync.WaitGroup)
	for _, route := range routesPkg {
		var r Route
		var found = false
		if err := json.Unmarshal([]byte(route), &r); err != nil {
			fmt.Printf("[shuttle] Bad route packet found: %s\n", err)
		} else {
			found = false
			routex.Lock()
			if routes[r.App+r.Space] != nil {
				for _, extr := range routes[r.App+r.Space] {
					if extr.DestinationUrl == r.DestinationUrl {
						found = true
					}
				}
			}
			routex.Unlock()

			// Explicitly disallow the test case url.
			if found == false && r.DestinationUrl != "syslog+tls://logs.apps.com:40841" {
				wg.Add(1)
				go func() {
					d, err := drains.Dial(kafkaGroup, r.DestinationUrl)
					if err == nil {
						var duplicate = false
						r.Destination = d
						routex.Lock()
						for _, sr := range routes[r.App+r.Space] {
							if sr.DestinationUrl == r.DestinationUrl {
								duplicate = true
							}
						}
						if duplicate == false {
							routes[r.App+r.Space] = append(routes[r.App+r.Space], r)
							fmt.Printf("[shuttle] Adding route: %s-%s -> %s\n", r.App, r.Space, r.DestinationUrl)
						} else {
							fmt.Printf("[shuttle] Not adding duplicate route: %s-%s -> %s\n", r.App, r.Space, r.DestinationUrl)
						}
						routex.Unlock()
					}
					wg.Done()
				}()
			}
		}
	}
	wg.Wait()
}

func GetSpacesToWatch(kafkaAddrs []string, kafkaGroup string) []string {
	spaces, err := GetKafkaTopics(kafkaAddrs, kafkaGroup)
	if err != nil {
		log.Fatalf("[shuttle] error: cannot get spaces: %s", err)
	}
	return Filter(spaces, func(v string) bool {
		return 	v != "kube-system" && 
				v != "alamoweblogs" && 
				v != "alamobuildlogs" && 
				!strings.HasPrefix(v, "subsystems-") && 
				!strings.HasSuffix(v, "-subsystems") && 
				!strings.HasPrefix(v, ".") && 
				!strings.HasPrefix(v, "_")
	})
}

func GetDrainById(client *Storage, Id string) (*Route, error) {
	routes_pkg, err := (*client).GetRoutes()
	if err != nil {
		return nil, err
	}
	for _, route := range routes_pkg {
		var r Route
		if err := json.Unmarshal([]byte(route), &r); err != nil {
			fmt.Printf("[shuttle] Bad route packet found: %s\n", err)
		} else {
			if r.Id == Id {
				return &r, nil
			}
		}
	}
	return nil, errors.New("No such drain found.")
}

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
		for _, route := range routes_pkg {
			var r Route
			if err := json.Unmarshal([]byte(route), &r); err != nil {
				ReportError(rr, err)
			} else {
				if r.App == app && r.Space == space {
					var n = LogDrainResponse{Addon: AddonResponse{Id: "", Name: ""}, CreatedAt: r.Created, UpdatedAt: r.Updated, Id: r.Id, Token: app + "-" + space, Url: r.DestinationUrl}
					resp = append(resp, n)
				}
			}
		}
		rr.JSON(200, resp)
	}
}

func CreateLogEvent(kafkaProducer sarama.AsyncProducer) func(martini.Params, LogSpec, binding.Errors, render.Render) {
	return func(params martini.Params, opts LogSpec, berr binding.Errors, r render.Render) {
		if berr != nil {
			ReportInvalidRequest(r)
			return
		}
		err := AddLogsToApp(kafkaProducer, opts)
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
		bytes, err := json.Marshal(Route{Id: id.String(), Space: space, App: app, DestinationUrl: opts.Url, Created: time.Now(), Updated: time.Now()})
		if err != nil {
			ReportError(r, err)
			return
		}
		err = (*client).AddRoute(string(bytes))
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
		var route, err = GetDrainById(client, params["id"])
		if err != nil {
			r.JSON(404, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		rb, err := json.Marshal(route)
		if err != nil {
			ReportError(r, err)
			return
		}
		err = (*client).RemoveRoute(string(rb))
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
		var route, err = GetDrainById(client, params["id"])
		if err != nil {
			r.JSON(404, map[string]interface{}{"message": "No such log drain or app found"})
			return
		}
		r.JSON(200, LogDrainResponse{Addon: AddonResponse{Id: "", Name: ""}, CreatedAt: route.Created, UpdatedAt: route.Updated, Id: route.Id, Token: app + "-" + space, Url: route.DestinationUrl})
	}
}

func CreateConsumer(kafkaAddrs []string, consumerGroup string) sarama.Consumer {
	config := sarama.NewConfig()
	config.Net.TLS.Enable = false
	config.ClientID = consumerGroup
	config.Consumer.Return.Errors = false

	err := config.Validate()
	if err != nil {
		log.Fatal(err)
	}

	consumer, err := sarama.NewConsumer(kafkaAddrs, config)
	if err != nil {
		log.Fatal(err)
	}
	return consumer
}

func CreateProducer(kafkaAddrs []string, consumerGroup string) sarama.AsyncProducer {
	config := sarama.NewConfig()

	config.Net.TLS.Enable = false
	config.Producer.Return.Errors = false
	config.ClientID = consumerGroup

	err := config.Validate()
	if err != nil {
		log.Fatal(err)
	}
	producer, err := sarama.NewAsyncProducer(kafkaAddrs, config)
	if err != nil {
		log.Fatal(err)
	}

	return producer
}

func GetKafkaTopics(kafkaAddrs []string, consumerGroup string) ([]string, error) {
	consumer := CreateConsumer(kafkaAddrs, consumerGroup)
	topics, err := consumer.Topics()
	consumer.Close()
	return topics, err
}

func StartHttpShuttleServices(client *Storage, kafkaAddrs []string, kafkaProducer sarama.AsyncProducer, port int) {
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
	m.Get("/octhc", HealthCheck(client, kafkaAddrs))
	// Private end point to create new events within the log stream that are controller-api specifc.
	m.Post("/log-events", binding.Json(LogSpec{}), CreateLogEvent(kafkaProducer))
	m.RunOnAddr(":" + strconv.FormatInt(int64(port), 10))
}

func StartShuttleServices(client *Storage, kafkaAddrs []string, port int, kafkaGroup string) {

	if os.Getenv("TEST_MODE") != "" {
		testMode = true
	}

	routex = &sync.Mutex{}
	routex.Lock()
	routes = make(map[string][]Route)
	routex.Unlock()

	// Load routes
	drains.InitSyslogDrains()
	RefreshRoutes(client, kafkaGroup)

	// Start kafka listening.
	fmt.Printf("[shuttle] Connecting to %s\n", strings.Join(kafkaAddrs, ","))

	// Create producer and consumers..
	spaces := GetSpacesToWatch(kafkaAddrs, kafkaGroup)
	producer := CreateProducer(kafkaAddrs, kafkaGroup)
	kafkaConsumer := CreateConsumerCluster(kafkaAddrs, kafkaGroup, spaces)
	kafkaConsumerWeblogs := CreateConsumerCluster(kafkaAddrs, kafkaGroup, []string{"alamoweblogs"})
	kafkaConsumerBuildlogs := CreateConsumerCluster(kafkaAddrs, kafkaGroup, []string{"alamobuildlogs"})

	fmt.Printf("[shuttle] Forwarding logs for spaces %s with group %s\n", spaces, kafkaGroup)

	// ensure close happens at some point.
	defer producer.Close()
	defer kafkaConsumer.Consumer.Close()
	defer kafkaConsumerWeblogs.Consumer.Close()
	defer kafkaConsumerBuildlogs.Consumer.Close()

	// Start http services
	go StartHttpShuttleServices(client, kafkaAddrs, producer, port)

	// Start listening to app logs
	go StartForwardingAppLogs(kafkaConsumer.Consumer)

	// Start listening to web logs
	go StartForwardingWebLogs(kafkaConsumerWeblogs.Consumer)

	// Start listening to web logs
	go StartForwardingBuildLogs(kafkaConsumerBuildlogs.Consumer)

	// Start drain tasks
	go drains.InitUrlDrains()

	t := time.NewTicker(time.Second * 60)
	for {
		fmt.Printf("[metrics] count#logs_sent=%d count#logs_received=%d count#failed_decode=%d count#goroutines=%d\n", messagesSent, messagesReceived, messageFailedDecode, runtime.NumGoroutine())
		drains.PrintMetrics()
		messagesSent = 0
		messagesReceived = 0
		messageFailedDecode = 0
		RefreshRoutes(client, kafkaGroup)
		<-t.C
	}
}
