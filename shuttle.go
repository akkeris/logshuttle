package main

import (
	"./drains"
	"./syslog"
	"encoding/json"
	"log"
	"runtime"
	"strings"
	"sync"
)

// TODO: Connect on demand (but deal with bad hosts will be tricky)
// TODO: Mark in storage errors connecting to syslog or drains

type Shuttle struct {
	sent          int
	received      int
	failed_decode int
	test_mode     bool
	routes        map[string][]Route
	routes_mutex  *sync.Mutex
	kafka_group   string
	kafka_addrs   string
	consumer      LogConsumer
	client        *Storage
}

func (sh *Shuttle) forwardAppLogs() {
	for e := range sh.consumer.AppLogs {
		var msg LogSpec
		sh.received++
		if err := json.Unmarshal(e.Value, &msg); err != nil {
			sh.failed_decode++
		} else {
			sh.SendMessage(msg)
		}
	}
}

func (sh *Shuttle) PrintMetrics() {
	log.Printf("[metrics] count#logs_sent=%d count#logs_received=%d count#failed_decode=%d count#goroutines=%d\n", sh.sent, sh.received, sh.failed_decode, runtime.NumGoroutine())
}

func (sh *Shuttle) Refresh() {
	sh.RefreshRoutes()
	sh.RefreshTopics()
}

func (sh *Shuttle) RefreshTopics() {
	sh.consumer.Refresh()
}

func (sh *Shuttle) forwardWebLogs() {
	for e := range sh.consumer.WebLogs {
		var msg LogSpec
		sh.received++
		if err := ParseWebLogMessage(e.Value, &msg); err == true {
			sh.failed_decode++
		} else {
			sh.SendMessage(msg)
		}
	}
}

func (sh *Shuttle) forwardBuildLogs() {
	for e := range sh.consumer.BuildLogs {
		var msg LogSpec
		sh.received++
		if err := ParseBuildLogMessage(e.Value, &msg); err == true {
			sh.failed_decode++
		} else {
			sh.SendMessage(msg)
		}
	}
}

func (sh *Shuttle) Init(client *Storage, kafkaAddrs []string, kafkaGroup string) error {
	log.Printf("[shuttle] Connecting to %s\n", strings.Join(kafkaAddrs, ","))
	sh.sent = 0
	sh.received = 0
	sh.failed_decode = 0
	sh.test_mode = false
	sh.client = client
	sh.routes_mutex = &sync.Mutex{}
	sh.routes_mutex.Lock()
	sh.routes = make(map[string][]Route)
	sh.routes_mutex.Unlock()
	sh.RefreshRoutes()
	sh.consumer.Init(kafkaAddrs, kafkaGroup)

	// Start listening to app logs
	go sh.forwardAppLogs()

	// Start listening to web logs
	go sh.forwardWebLogs()

	// Start listening to build logs
	go sh.forwardBuildLogs()
	return nil
}

func (sh *Shuttle) SendMessage(message LogSpec) {
	proc := Process{App: message.Kubernetes.ContainerName, Type: "web"}

	if strings.Index(message.Kubernetes.ContainerName, "--") != -1 {
		var components = strings.SplitN(message.Kubernetes.ContainerName, "--", 2)
		proc = Process{App: components[0], Type: components[1]}
	}

	sh.routes_mutex.Lock()
	r := sh.routes[proc.App+message.Topic]
	sh.routes_mutex.Unlock()

	for _, route := range r {
		tag := proc.Type + "." + strings.Replace(strings.Replace(message.Kubernetes.PodName, "-"+proc.Type+"-", "", 1), proc.App+"-", "", 1)
		if strings.HasPrefix(message.Kubernetes.PodName, "akkeris/") {
			tag = message.Kubernetes.PodName
		}
		var host = proc.App + "-" + message.Topic
		if sh.test_mode {
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
		sh.sent++
	}
}

func (sh *Shuttle) Close() {
	sh.consumer.Close()
}

func (sh *Shuttle) EnableTestMode() {
	sh.test_mode = true
}

func (sh *Shuttle) RefreshRoutes() {
	routesPkg, err := (*sh.client).GetRoutes()
	if err != nil {
		log.Fatalf("[shuttle] error: cannot obtain routes: %s", err)
		return
	}

	wg := new(sync.WaitGroup)
	for _, r := range routesPkg {
		var found = false
		sh.routes_mutex.Lock()
		if sh.routes[r.App+r.Space] != nil {
			for _, extr := range sh.routes[r.App+r.Space] {
				if extr.DestinationUrl == r.DestinationUrl {
					found = true
				}
			}
		}
		sh.routes_mutex.Unlock()

		// Explicitly disallow the test case url.
		if found == false && r.DestinationUrl != "syslog+tls://logs.apps.com:40841" {
			wg.Add(1)
			go func() {
				d, err := drains.Dial(sh.kafka_group, r.DestinationUrl)
				if err == nil {
					var duplicate = false
					r.Destination = d
					sh.routes_mutex.Lock()
					for _, sr := range sh.routes[r.App+r.Space] {
						if sr.DestinationUrl == r.DestinationUrl {
							duplicate = true
						}
					}
					if duplicate == false {
						sh.routes[r.App+r.Space] = append(sh.routes[r.App+r.Space], r)
						log.Printf("[shuttle] Adding route: %s-%s -> %s\n", r.App, r.Space, r.DestinationUrl)
					} else {
						log.Printf("[shuttle] Not adding duplicate route: %s-%s -> %s\n", r.App, r.Space, r.DestinationUrl)
					}
					sh.routes_mutex.Unlock()
				}
				wg.Done()
			}()
		}
	}
	wg.Wait()
}
