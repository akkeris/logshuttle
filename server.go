package main

import (
	"os"
	"log"
	"strings"
	"strconv"
	"net/http"
	"net/http/pprof"

)

func main() {
	var kafkaGroup = "logshuttle"
	// Get kafka group for testing.
	if os.Getenv("TEST_MODE") != "" {
		log.Printf("Using kafka group logshuttle-testing for testing purposes...")
		kafkaGroup = "logshuttletest"
	}

	// Connect to storage (usually redis) instance
	var storage RedisStorage
	storage.Init(strings.Replace(os.Getenv("REDIS_URL"), "redis://", "", 1))

	var s Storage = &storage
	// Connect to kafka instance
	kafkaAddrs := strings.Split(os.Getenv("KAFKA_HOSTS"), ",")

	// Get the port for http services.
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
