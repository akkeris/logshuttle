package shuttle

import (
	"encoding/json"
	kafka "github.com/confluentinc/confluent-kafka-go/kafka"
	"log"
	"github.com/akkeris/logshuttle/events"
	"math/rand"
	"net/http"
	"strings"
	"time"
	"os"
)


// Boilerplate random string generator
var randomSource = rand.NewSource(time.Now().UnixNano())

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

func RandomString(n int) string {
	b := make([]byte, n)

	// A randomSource.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, randomSource.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = randomSource.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

type Session struct {
	IsOpen   bool
	loops    int
	response http.ResponseWriter
	app      string
	space    string
	site     string
	group    string
}

func WriteAndFlush(log string, res http.ResponseWriter) error {
	_, err := res.Write([]byte(log))
	if err != nil {
		return err
	} else if f, ok := res.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func (ls *Session) RespondWithAppLog(e *kafka.Message) error {
	var msg events.LogSpec
	if err := json.Unmarshal(e.Value, &msg); err == nil {
		if IsAppMatch(msg.Kubernetes.ContainerName, ls.app) && msg.Topic == ls.space {
			ls.loops = 0
			proc := ContainerToProc(msg.Kubernetes.ContainerName)
			log := msg.Time.UTC().Format(time.RFC3339) + " " + ls.app + "-" + ls.space + " app[" + proc.Type + "." + strings.Replace(strings.Replace(msg.Kubernetes.PodName, "-"+proc.Type+"-", "", 1), proc.App+"-", "", 1) + "]: " + strings.TrimSpace(KubernetesToHumanReadable(msg.Log)) + "\n"
			if err = WriteAndFlush(log, ls.response); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ls *Session) RespondWithIstioWebLog(e *kafka.Message) error {
	var msg events.LogSpec
	if ParseIstioWebLogMessage(e.Value, &msg) == false && ((IsAppMatch(msg.Kubernetes.ContainerName, ls.app) && msg.Topic == ls.space) || (msg.Site != "" && msg.Site == ls.site)) {
		ls.loops = 0
		if msg.Site == "" {
			log := msg.Time.UTC().Format(time.RFC3339) + " " + ls.app + "-" + ls.space + " akkeris/router: " + msg.Log + " host=" + msg.Kubernetes.ContainerName + " path=" + msg.Path + "\n"
			if err := WriteAndFlush(log, ls.response); err != nil {
				return err
			}
		} else {
			log := msg.Time.UTC().Format(time.RFC3339) + " " + msg.Site + " akkeris/router: " + msg.Log + " host=" + msg.Site + " path=" + msg.SitePath + "\n"
			if err := WriteAndFlush(log, ls.response); err != nil {
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
			if err := WriteAndFlush(log, ls.response); err != nil {
				return err
			}
		} else {
			log := msg.Time.UTC().Format(time.RFC3339) + " " + msg.Site + " akkeris/router: " + msg.Log + " host=" + msg.Site + " path=" + msg.SitePath + "\n"
			if err := WriteAndFlush(log, ls.response); err != nil {
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
		if err := WriteAndFlush(log, ls.response); err != nil {
			return err
		}
	}
	return nil
}

func (ls *Session) ConsumeAndRespond(kafkaAddrs []string, app string, space string, site string, req *http.Request, res http.ResponseWriter) {
	var subject string
	if site != "" {
		subject = app + "-" + space
	} else {
		subject = site
	}

	ls.group = RandomString(16)
	ls.response = res
	ls.space = space
	ls.app = app
	ls.site = site
	ls.loops = 0
	ls.IsOpen = true

	debug := false 
	if os.Getenv("DEBUG_SESSION") == "true" {
		debug = true
	}

	consumer := events.CreateConsumerCluster(kafkaAddrs, ls.group)
	if ls.site == "" && ls.space != "" {
		consumer.SubscribeTopics([]string{ls.space, "alamoweblogs", "istio-access-logs", "alamobuildlogs"}, nil)
	} else if ls.site != "" {
		consumer.SubscribeTopics([]string{"alamoweblogs", "istio-access-logs"}, nil)
	} else {
		consumer.Close()
		return
	}
	log.Printf("[info] listening for logs on %s[group: %s]\n", subject, ls.group)
	defer consumer.Close()

	for ls.IsOpen == true {
		ev := consumer.Poll(100)
		// the context for the request is in error, it most likely went away.
		if req.Context().Err() != nil {
			ls.IsOpen = false
			if debug {
				log.Printf("[debug] request context went away %s[group: %s]\n", subject, ls.group)
			}
			continue
		}
		// we've timed out, 1 minute with no messages.
		if ls.loops > 10*60 {
			ls.IsOpen = false
			if debug {
				log.Printf("[debug] request context timed out %s[group: %s]\n", subject, ls.group)
			}
			continue
		}
		// no message was found after 100ms.
		if ev == nil {
			ls.loops = ls.loops + 1
			continue
		}
		switch e := ev.(type) {
		case *kafka.Message:
			if ls.space != "" && *e.TopicPartition.Topic == ls.space {
				if err := ls.RespondWithAppLog(e); err != nil {
					if debug {
						log.Printf("[debug] write and flush failed in app logs [%s] %s[group: %s]\n", err.Error(), subject, ls.group)
					}
					ls.IsOpen = false
					break
				}
			} else if *e.TopicPartition.Topic == "alamoweblogs" {
				if err := ls.RespondWithWebLog(e); err != nil {
					if debug {
						log.Printf("[debug] write and flush failed in f5 web logs [%s] %s[group: %s]\n", err.Error(), subject, ls.group)
					}
					ls.IsOpen = false
					break
				}
			} else if *e.TopicPartition.Topic == "istio-access-logs" {
				if err := ls.RespondWithIstioWebLog(e); err != nil {
					if debug {
						log.Printf("[debug] write and flush failed in istio app logs [%s] %s[group: %s]\n", err.Error(), subject, ls.group)
					}
					ls.IsOpen = false
					break
				}
			} else if ls.space != "" && *e.TopicPartition.Topic == "alamobuildlogs" {
				if err := ls.RespondWithBuildLog(e); err != nil {
					if debug {
						log.Printf("[debug] write and flush failed in build logs [%s] %s[group: %s]\n", err.Error(), subject, ls.group)
					}
					ls.IsOpen = false
					break
				}
			}
		case kafka.Error:
			log.Printf("%% Error: %v\n", e)
			ls.IsOpen = false
			return
		default:
			// An unknown kafka message meta message type.
			ls.loops = ls.loops + 1
		}
	}
	log.Printf("[info] closing listener for %s[group: %s]\n", subject, ls.group)
}
