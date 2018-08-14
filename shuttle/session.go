package shuttle

import (
	"../events"
	"encoding/json"
	"fmt"
	kafka "github.com/confluentinc/confluent-kafka-go/kafka"
	"net/http"
	"strings"
	"time"
)

type Session struct {
	IsOpen   bool
	loops    int
	response http.ResponseWriter
	app      string
	space    string
	site     string
	group    string
}

func (ls *Session) RespondWithAppLog(e *kafka.Message) error {
	var msg events.LogSpec
	if err := json.Unmarshal(e.Value, &msg); err == nil {
		if IsAppMatch(msg.Kubernetes.ContainerName, ls.app) && msg.Topic == ls.space {
			ls.loops = 0
			proc := ContainerToProc(msg.Kubernetes.ContainerName)
			log := msg.Time.UTC().Format(time.RFC3339) + " " + ls.app + "-" + ls.space + " app[" + proc.Type + "." + strings.Replace(strings.Replace(msg.Kubernetes.PodName, "-"+proc.Type+"-", "", 1), proc.App+"-", "", 1) + "]: " + strings.TrimSpace(KubernetesToHumanReadable(msg.Log)) + "\n"
			err = WriteAndFlush(log, ls.response)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ls *Session) RespondWithWebLog(e *kafka.Message) error {
	var msg events.LogSpec
	if ParseWebLogMessage(e.Value, &msg) == false && ((IsAppMatch(msg.Kubernetes.ContainerName, ls.app) && msg.Topic == ls.space) || (msg.Site != "" && msg.Site == ls.site)) {
		ls.loops = 0
		if msg.Site == "" {
			log := msg.Time.UTC().Format(time.RFC3339) + " " + ls.app + "-" + ls.space + " akkeris/router: " + msg.Log + " host=" + msg.Kubernetes.ContainerName + " path=" + msg.Path + "\n"
			err := WriteAndFlush(log, ls.response)
			if err != nil {
				return err
			}
		} else {
			log := msg.Time.UTC().Format(time.RFC3339) + " " + msg.Site + " akkeris/router: " + msg.Log + " host=" + msg.Site + " path=" + msg.SitePath + "\n"
			err := WriteAndFlush(log, ls.response)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ls *Session) RespondWithBuildLog(e *kafka.Message) error {
	var msg events.LogSpec
	if ParseBuildLogMessage(e.Value, &msg) == false && IsAppMatch(msg.Kubernetes.ContainerName, ls.app) && msg.Topic == ls.space {
		ls.loops = 0
		log := msg.Time.UTC().Format(time.RFC3339) + " " + ls.app + "-" + ls.space + " akkeris/build: " + msg.Log + "\n"
		err := WriteAndFlush(log, ls.response)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ls *Session) ConsumeAndRespond(kafkaAddrs []string, app string, space string, site string, res http.ResponseWriter) {
	if site != "" {
		fmt.Println("[info] listening for logs on site " + site)
	} else {
		fmt.Println("[info] listening for logs on " + app + "-" + space)
	}

	ls.group = RandomString(16)
	ls.response = res
	ls.space = space
	ls.app = app
	ls.site = site
	ls.loops = 0
	ls.IsOpen = true

	consumer := events.CreateConsumerCluster(kafkaAddrs, ls.group)
	consumer.SubscribeTopics([]string{ls.space, "alamoweblogs", "alamobuildlogs"}, nil)
	defer consumer.Close()

	for ls.IsOpen == true {
		ev := consumer.Poll(100)
		if ls.loops > 10*60 {
			// we've timed out.
			ls.IsOpen = false
			continue
		}
		if ev == nil {
			ls.loops = ls.loops + 1
			continue
		}
		switch e := ev.(type) {
		case *kafka.Message:
			if ls.space != "" && *e.TopicPartition.Topic == ls.space {
				if err := ls.RespondWithAppLog(e); err != nil {
					ls.IsOpen = false
					break
				}
			} else if *e.TopicPartition.Topic == "alamoweblogs" {
				if err := ls.RespondWithWebLog(e); err != nil {
					ls.IsOpen = false
					break
				}
			} else if ls.space != "" && *e.TopicPartition.Topic == "alamobuildlogs" {
				if err := ls.RespondWithBuildLog(e); err != nil {
					ls.IsOpen = false
					break
				}
			}
		case kafka.Error:
			fmt.Printf("%% Error: %v\n", e)
			ls.IsOpen = false
			return
		default:
			ls.loops = ls.loops + 1
		}
	}
	if site != "" {
		fmt.Println("[info] closing listener on site " + site)
	} else {
		fmt.Println("[info] closing listener on " + ls.app + "-" + ls.space)
	}
}
