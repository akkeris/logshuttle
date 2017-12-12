package main

import (
	"os"
	"log"
	"strings"
	"strconv"
)

func main() {
	var kafkaGroup = "logshuttle"
	// Get kafka group for testing.
	if os.Getenv("TEST_MODE") != "" {
		log.Printf("Using kafka group logshuttle-testing for testing purposes...")
		kafkaGroup = "logshuttle-testing"
	}
	// Get logging logger destination
	syslogEnv := os.Getenv("SYSLOG")
	if syslogEnv != "" {
		// Connect to our logging end point
		ConnectOurLogging(kafkaGroup, syslogEnv)
	}

	// Connect to redis instance
	client := GetRedis()

	// Connect to kafka instance
	kafkaAddrs := strings.Split(os.Getenv("KAFKA_HOSTS"), ",")

	// Get the port for http services.
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		port = 5000
	}

	if os.Getenv("RUN_SESSION") != "" {
		StartSessionServices(client, kafkaAddrs, port)
	} else {
		StartShuttleServices(client, kafkaAddrs, port, kafkaGroup)
	}
}
