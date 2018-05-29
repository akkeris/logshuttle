package main

import (
	"errors"
	kafka "github.com/confluentinc/confluent-kafka-go/kafka"
	"log"
	"strings"
)

func CreateConsumerCluster(kafkaAddrs []string, kafkaGroup string) *kafka.Consumer {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":       strings.Join(kafkaAddrs, ","),
		"group.id":                kafkaGroup,
		"enable.auto.commit":      true,
		"auto.commit.interval.ms": 1000,
		"session.timeout.ms":      30000,
		"socket.keepalive.enable": true,
	})
	if err != nil {
		log.Fatal(err)
	}
	return c
}

type LogConsumer struct {
	kafkaConsumer *kafka.Consumer
	AppLogs       chan *kafka.Message
	BuildLogs     chan *kafka.Message
	WebLogs       chan *kafka.Message
	IsOpen        bool
	address       []string
	group         string
}

func (lc *LogConsumer) Init(kafkaAddrs []string, kafkaGroup string) {
	lc.address = kafkaAddrs
	lc.group = kafkaGroup
	lc.Open()
}

func (lc *LogConsumer) MarkOffset(msg *kafka.Message) {
	lc.kafkaConsumer.CommitMessage(msg)
}

func (lc *LogConsumer) RunPooler() {
	for lc.IsOpen == true {
		ev := lc.kafkaConsumer.Poll(100)
		if ev == nil {
			continue
		}
		switch msg := ev.(type) {
		case *kafka.Message:
			if strings.HasPrefix(*msg.TopicPartition.Topic, "_") == true {
				continue
			} else if *msg.TopicPartition.Topic == "alamoweblogs" {
				lc.WebLogs <- msg
			} else if *msg.TopicPartition.Topic == "alamobuildlogs" {
				lc.BuildLogs <- msg
			} else {
				lc.AppLogs <- msg
			}
		case kafka.Error:
			log.Printf("%% Error: %v\n", msg)
			lc.Close()
		default:
			// do nothing, ignore the message.
		}
	}
}

func (lc *LogConsumer) Refresh() error {
	err := lc.kafkaConsumer.SubscribeTopics([]string{"^.*$"}, nil)
	if err != nil {
		log.Println("Error listening to all topics", err)
	}
	return err
}

func (lc *LogConsumer) Open() error {
	if lc.IsOpen == true {
		return errors.New("Unable to open log consumer, its already open.")
	}
	if lc.address == nil || len(lc.address) == 0 {
		return errors.New("invalid address")
	}
	if lc.group == "" {
		return errors.New("invalid group")
	}
	lc.kafkaConsumer = CreateConsumerCluster(lc.address, lc.group)
	err := lc.kafkaConsumer.SubscribeTopics([]string{"^.*$"}, nil)
	if err != nil {
		log.Println("Error listening to all topics", err)
	}
	lc.AppLogs = make(chan *kafka.Message)
	lc.BuildLogs = make(chan *kafka.Message)
	lc.WebLogs = make(chan *kafka.Message)
	lc.IsOpen = true
	go lc.RunPooler()
	return nil
}

func (lc *LogConsumer) Close() {
	if lc.IsOpen == false {
		return
	}
	lc.IsOpen = false
	lc.kafkaConsumer.Close()
}
