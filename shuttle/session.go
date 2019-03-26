package shuttle

import (
	"encoding/json"
	kafka "github.com/confluentinc/confluent-kafka-go/kafka"
	"log"
	"logshuttle/events"
	"net/http"
	"strconv"
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

type IstioLog struct {
	Time      time.Time `json:"time"`
	Severity  string    `json:"severity"`
	Bytes     int       `json:"bytes"`
	Method    string    `json:"method"`
	Source    string    `json:"source"`
	Space     string    `json:"space"`
	Path      string    `json:"path"`
	RequestId string    `json:"request_id"`
	From      string    `json:"from"`
	Host      string    `json:"host"`
	App       string    `json:"app"`
	Fwd       string    `json:"fwd"`
	Status    int       `json:"status"`
	Service   string    `json:"service"`
	Dyno      string    `json:"dyno"`
	Total     string    `json:"total"`
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

func (ls *Session) RespondWithIstioWebLog(e *kafka.Message) error {
	var msg IstioLog
	if err := json.Unmarshal(e.Value, &msg); err == nil {
		if msg.App == ls.app && msg.Space == ls.space {
			ls.loops = 0
			log := e.Timestamp.UTC().Format(time.RFC3339) + " " + ls.app + "-" + ls.space + " akkers/router[" + msg.Dyno + "]: " +
				"bytes=" + strconv.Itoa(msg.Bytes) + " " +
				"method=" + msg.Method + " " +
				"path=" + msg.Path + " " +
				"request_id=" + msg.RequestId + " " +
				"host=" + msg.Host + " " +
				"fwd=" + msg.Fwd + " " +
				"status=" + strconv.Itoa(msg.Status) + " " +
				"service=" + msg.Service + " " +
				"total=" + msg.Total + " " +
				"source=" + msg.Total + "\n"
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
		log.Println("[info] listening for logs on site " + site)
	} else {
		log.Println("[info] listening for logs on " + app + "-" + space)
	}

	ls.group = RandomString(16)
	ls.response = res
	ls.space = space
	ls.app = app
	ls.site = site
	ls.loops = 0
	ls.IsOpen = true

	consumer := events.CreateConsumerCluster(kafkaAddrs, ls.group)
	if ls.site == "" && ls.space != "" {
		consumer.SubscribeTopics([]string{ls.space, "alamoweblogs", "istio-access-logs", "alamobuildlogs"}, nil)
	} else if ls.site != "" {
		consumer.SubscribeTopics([]string{"alamoweblogs", "istio-access-logs"}, nil)
	} else {
		consumer.Close()
		return
	}
	defer consumer.Close()

	for ls.IsOpen == true {
		ev := consumer.Poll(100)
		if ls.loops > 10*60*5 {
			// we've timed out, 5 minutes.
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
			} else if *e.TopicPartition.Topic == "istio-access-logs" {
				if err := ls.RespondWithIstioWebLog(e); err != nil {
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
			log.Printf("%% Error: %v\n", e)
			ls.IsOpen = false
			return
		default:
			ls.loops = ls.loops + 1
		}
	}
	if site != "" {
		log.Println("[info] closing listener on site " + site)
	} else {
		log.Println("[info] closing listener on " + ls.app + "-" + ls.space)
	}
}
