package shuttle

import (
	"encoding/json"
	"log"
	"logshuttle/drains"
	"logshuttle/events"
	"logshuttle/storage"
	"logshuttle/syslog"
	"runtime"
	"strings"
	"sync"
)

// TODO: Connect on demand (but deal with bad hosts will be tricky)
// TODO: Mark in storage errors connecting to syslog or drains

type Destination struct {
	route storage.Route
	drain drains.Drain
}

type Shuttle struct {
	sent          int
	received      int
	failed_decode int
	test_mode     bool
	routes        map[string][]Destination
	route_keys    []string
	routes_mutex  *sync.Mutex
	kafka_group   string
	kafka_addrs   string
	consumer      events.LogConsumer
	client        *storage.Storage
}

func (sh *Shuttle) PrintMetrics() {
	log.Printf("[metrics] count#logs_sent=%d count#logs_received=%d count#failed_decode=%d count#goroutines=%d\n", sh.sent, sh.received, sh.failed_decode, runtime.NumGoroutine())
	sh.sent = 0
	sh.received = 0
	sh.failed_decode = 0
}

func (sh *Shuttle) Refresh() {
	sh.RefreshRoutes()
	sh.RefreshTopics()
}

func (sh *Shuttle) RefreshTopics() {
	sh.consumer.Refresh()
}

func (sh *Shuttle) forwardAppLogs() {
	for e := range sh.consumer.AppLogs {
		var msg events.LogSpec
		sh.received++
		if err := json.Unmarshal(e.Value, &msg); err != nil {
			sh.failed_decode++
		} else {
			sh.SendMessage(msg)
		}
	}
}

func (sh *Shuttle) forwardWebLogs() {
	for e := range sh.consumer.WebLogs {
		var msg events.LogSpec
		sh.received++
		if err := ParseWebLogMessage(e.Value, &msg); err == true {
			sh.failed_decode++
		} else {
			var orgLog = msg.Log
			msg.Log = msg.Log + " host=" + msg.Kubernetes.ContainerName + "-" + msg.Topic + " path=" + msg.Path
			sh.SendMessage(msg)
			if msg.Site != "" {
				msg.Log = orgLog + " host=" + msg.Site + " path=" + msg.SitePath
				msg.Kubernetes.PodName = "akkeris/router"
				msg.Kubernetes.ContainerName = msg.Site
				msg.Topic = ""
				sh.SendMessage(msg)
			}
		}
	}
}

func (sh *Shuttle) forwardBuildLogs() {
	for e := range sh.consumer.BuildLogs {
		var msg events.LogSpec
		sh.received++
		if err := ParseBuildLogMessage(e.Value, &msg); err == true {
			sh.failed_decode++
		} else {
			sh.SendMessage(msg)
		}
	}
}

func (sh *Shuttle) Init(client *storage.Storage, kafkaAddrs []string, kafkaGroup string) error {
	log.Printf("[shuttle] Connecting to %s\n", strings.Join(kafkaAddrs, ","))
	sh.sent = 0
	sh.received = 0
	sh.failed_decode = 0
	sh.test_mode = false
	sh.client = client
	sh.routes_mutex = &sync.Mutex{}
	sh.routes_mutex.Lock()
	sh.routes = make(map[string][]Destination)
	sh.route_keys = make([]string, 0)
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

func (sh *Shuttle) SendMessage(message events.LogSpec) {
	proc := events.Process{App: message.Kubernetes.ContainerName, Type: "web"}

	if strings.Index(message.Kubernetes.ContainerName, "--") != -1 {
		var components = strings.SplitN(message.Kubernetes.ContainerName, "--", 2)
		proc = events.Process{App: components[0], Type: components[1]}
	}

	sh.routes_mutex.Lock()
	r := sh.routes[proc.App+message.Topic]
	sh.routes_mutex.Unlock()
	for _, d := range r {
		tag := proc.Type + "." + strings.Replace(strings.Replace(message.Kubernetes.PodName, "-"+proc.Type+"-", "", 1), proc.App+"-", "", 1)
		if strings.HasPrefix(message.Kubernetes.PodName, "akkeris/") {
			tag = message.Kubernetes.PodName
		}
		var host = proc.App + "-" + message.Topic
		if message.Topic == "" {
			host = proc.App
		}
		if sh.test_mode {
			host = "logshuttle-test"
		}
		var severity = syslog.SevInfo
		if message.Stream == "stderr" {
			severity = syslog.SevErr
		}
		var p = syslog.Packet{
			Severity: severity,
			Facility: syslog.LogUser,
			Hostname: host,
			Tag:      tag,
			Time:     message.Time,
			Message:  KubernetesToHumanReadable(message.Log),
		}
		d.drain.Packets() <- p
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

	// Add new routes not found.
	wg := new(sync.WaitGroup)
	for _, rt := range routesPkg {
		var found = false
		var route_key = rt.GetRouteKey()
		sh.routes_mutex.Lock()
		if sh.routes[route_key] != nil {
			for _, extr := range sh.routes[route_key] {
				if extr.route.Id == rt.Id {
					found = true
				}
			}
		}
		sh.routes_mutex.Unlock()
		// Explicitly disallow the test case url.
		if found == false && rt.DestinationUrl != "syslog+tls://logs.apps.com:40841" {
			wg.Add(1)
			go func(rts storage.Route) {
				var duplicate = false
				var rts_route_key = rts.GetRouteKey()
				sh.routes_mutex.Lock()
				for _, sr := range sh.routes[rts_route_key] {
					if sr.drain.Url() == rts.DestinationUrl {
						duplicate = true
					}
				}
				sh.routes_mutex.Unlock()
				if duplicate == false {
					d, err := drains.Dial(rts.Id, rts.DestinationUrl)
					if err == nil {
						sh.routes_mutex.Lock()
						sh.routes[rts_route_key] = append(sh.routes[rts_route_key], Destination{drain: d, route: rts})
						var found_key = false
						for _, v := range sh.route_keys {
							if v == rts.GetRouteKey() {
								found_key = true
							}
						}
						if found_key == false {
							sh.route_keys = append(sh.route_keys, rts_route_key)
						}
						sh.routes_mutex.Unlock()
						log.Printf("[shuttle] Adding route: %s with key\n", rts.GetRouteString())
					} else if err.Error() != "Host is part of a bad host list." {
						log.Printf("[shuttle] Cannot add route: %s, (%s) will retry in 5 minutes\n", rts.GetRouteString(), err.Error())
					}
				} else {
					log.Printf("[shuttle] Not adding duplicate route: %s\n", rts.GetRouteString())
				}
				wg.Done()
			}(rt)
		}
	}
	wg.Wait()

	// Remove routes no longer in storage
	sh.routes_mutex.Lock()
	for _, route_key := range sh.route_keys {
		if destinations, ok := sh.routes[route_key]; ok && len(destinations) > 0 {
			for ndx, destination := range destinations {
				var found = false
				for _, rt := range routesPkg {
					if rt.Id == destination.route.Id {
						found = true
					}
				}
				if found == false {
					log.Printf("[shuttle] Removing route: %s\n", destination.route.GetRouteString())
					err := drains.Undial(destination.drain.Id(), destination.drain.Url())
					if len(destinations) == 1 {
						sh.routes[route_key] = make([]Destination, 0)
					} else {
						sh.routes[route_key] = append(destinations[:ndx], destinations[ndx+1:]...)
					}
					if err != nil {
						log.Printf("[shuttle] Unable to remove stale drains for %s and %s\n", destination.drain.Id(), destination.drain.Url())
					}
				}
			}
		}
	}
	sh.routes_mutex.Unlock()

}
