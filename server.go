package main

import (
	"github.com/akkeris/logshuttle/storage"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"
	"github.com/stackimpact/stackimpact-go"
	"log"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"
)

func ReportInvalidRequest(r render.Render) {
	r.JSON(http.StatusOK, "Malformed Request")
}

func ReportError(r render.Render, err error) {
	log.Printf("error: %s", err)
	r.JSON(http.StatusInternalServerError, map[string]interface{}{"message": "Internal Server Error"})
}

func HealthCheck(client *storage.Storage) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		err := (*client).HealthCheck()
		if err != nil {
			log.Printf("error: %s", err)
			res.WriteHeader(http.StatusOK)
			res.Write([]byte("overall_status=bad,redis_check=failed"))
		} else {
			res.WriteHeader(http.StatusOK)
			res.Write([]byte("overall_status=good"))
		}
	}
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
