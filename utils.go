package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"net/http"
	"github.com/martini-contrib/render"
	"github.com/bsm/sarama-cluster"
	"gopkg.in/redis.v4"
	"github.com/go-martini/martini"
	"github.com/trevorlinton/remote_syslog2/syslog"
)

// Boilerplate random string generator
var randomSource = rand.NewSource(time.Now().UnixNano())

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var ourLogger *syslog.Logger
var serverName string

func CreateConsumerCluster(kafkaAddrs []string, consumerGroup string, topics []string) KafkaConsumer {
	config := cluster.NewConfig()
	config.Net.TLS.Enable = false
	config.ClientID = consumerGroup
	config.Net.MaxOpenRequests = 200        // allow up to 200 open requests at a time, default is 5
	config.Net.KeepAlive = time.Second * 30 // keep the connection open for 30 seconds before we hang up. default is no keep alive.
	config.Group.PartitionStrategy = cluster.StrategyRoundRobin
	config.Consumer.Return.Errors = false
	config.Group.Return.Notifications = false

	client, err := cluster.NewClient(kafkaAddrs, config)
	if err != nil {
		log.Fatal(err)
	}

	err_v := config.Validate()
	if err_v != nil {
		log.Fatal(err_v)
	}

	consumer, err_c := cluster.NewConsumerFromClient(client, consumerGroup, topics)
	if err_c != nil {
		log.Fatal(err_c)
	}
	return KafkaConsumer{Consumer: consumer, Config: config, Client: client}
}

func ConnectOurLogging(kafkaGroup string, syslogger string) {
	serverName = kafkaGroup
	if os.Getenv("TEST_MODE") == "" {
		log.Printf("Connecting to logger.")
		dest, err := syslog.Dial(kafkaGroup,
			"tls", syslogger, nil, time.Second*60, time.Second*60, 99990)
		if err != nil {
			log.Fatalf("error: cannot connect to our logging destination: %s", err)
		}
		log.Printf("Logger successfully connected.")
		ourLogger = dest
	}
}

func GetRedis() *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     strings.Replace(os.Getenv("REDIS_URL"), "redis://", "", 1),
		Password: "",
		DB:       0,
	})
	return client
}

func ReportInvalidRequest(r render.Render) {
	r.JSON(400, "Malformed Request")
}

func ReportError(r render.Render, err error) {
	DumpToSyslog("error: %s", err)
	r.JSON(500, map[string]interface{}{"message": "Internal Server Error"})
}

func DumpToSyslog(format string, v ...interface{}) {
	var msg = fmt.Sprintf(format, v...)
	var p = syslog.Packet{
		Severity: syslog.LogUser,
		Facility: syslog.SevInfo,
		Hostname: serverName + "-default-" + os.Getenv("ENV"),
		Tag:      "web",
		Time:     time.Now(),
		Message:  msg,
	}
	if os.Getenv("TEST_MODE") != "" {
		log.Printf("%s", p.Generate(1024*20))
	} else {
		ourLogger.Packets <- p
	}
}

func Filter(vs []string, f func(string) bool) []string {
	vsf := make([]string, 0)
	for _, v := range vs {
		if f(v) {
			vsf = append(vsf, v)
		}
	}
	return vsf
}

func IsAppMatch(potential string, app_name string) bool {
	return potential == app_name || strings.HasPrefix(potential, app_name+"--")
}


func HealthCheck(client *redis.Client, kafkaAddrs []string) func(http.ResponseWriter, *http.Request, martini.Params) {
	return func(res http.ResponseWriter, req *http.Request, params martini.Params) {
		_, err := client.Info("all").Result()
		if err != nil {
			DumpToSyslog("error: %s", err)
			res.WriteHeader(200)
			res.Write([]byte("overall_status=bad,redis_check=failed"))
		} else {
			res.WriteHeader(200)
			res.Write([]byte("overall_status=good"))
		}
	}
}

func KubernetesToHumanReadable(message string) string {
	// early bail out.
	if !strings.HasPrefix(message, "Phase: ") {
		return message
		// Phase: Creating -- Creating pod blog-1050769379-6ktnc
	} else if strings.HasPrefix(message, "Phase: Creating -- ") {
		return "Creating Dyno"
		//Phase: Pending/ --
	} else if strings.HasPrefix(message, "Phase: Pending/ --") {
		return "Waiting on Dyno"
		//Phase: Pending/waiting --  reason=ContainerCreating
	} else if strings.HasPrefix(message, "Phase: Pending/waiting --") {
		return "Waiting on Dyno"
		// Phase: Running/waiting --  reason=CrashLoopBackOff message=Back-off 5m0s restarting failed container=useraccount pod=useraccount-3805495696-7lrj3_perf-stg(60fa2fa3-4d3a-11e7-82b9-02ef34ee5340)
	} else if strings.HasPrefix(message, "Phase: Running/waiting --") {
		return "at=error code=H10 desc=\"App crashed\""
		//Phase: Running/running --  startedAt=2017-06-16T16:16:30
	} else if strings.HasPrefix(message, "Phase: Running/running --") {
		return "Checking Dyno Health"
		//Stopping Dyno (Phase: Running/terminated --  reason=Error startedAt=2017-06-16T16:12:39Z finishedAt=2017-06-16T16:17:09Z containerID=docker://5d822d1bfc8ea75a3fc5a04c6d3ce319da62f0fde078646ab1fea760199a8f59 exitCode=137
	} else if strings.HasPrefix(message, "Phase: Running/terminated --") {
		re := regexp.MustCompile("exitCode=([1-9]+)")
		return "Dyno exited (" + strings.Replace(re.FindString(message), "exitCode=", "exit code ", 1) + ")"
		//Phase: Deleting -- pod blog-472348638-9tzlx in space default
	} else if strings.HasPrefix(message, "Phase: Deleting -- pod") {
		return "Deleting Dyno"
	} else {
		return message
	}
}

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

func ParseWebLogMessage(data []byte, msg *LogSpec) (bool) {
	var app = ""
	var space = ""
	var reformattedMessage = ""
	var message = string(data)

	for _, block := range strings.Fields(message) {
		var value = strings.SplitN(block, "=", 2)
		if len(value) < 2 {
			return true
		} else {
			if value[0] == "hostname" {
				urls := strings.Split(value[1], ".")
				app = urls[0]
				var splitAppName = strings.SplitN(app, "-", 2)
				app = splitAppName[0]
				if len(splitAppName) == 1 {
					space = "default"
				} else {
					space = splitAppName[1]
				}
				reformattedMessage = reformattedMessage + "host=" + value[1] + " "
			} else if value[0] == "source" {
				unescaped, err := url.QueryUnescape(value[1])
				if err != nil {
					reformattedMessage = reformattedMessage + "fwd=\"" + value[1] + "\" "
				} else {
					reformattedMessage = reformattedMessage + "fwd=\"" + unescaped + "\" "
				}
			} else if value[0] != "timestamp" {
				reformattedMessage = reformattedMessage + value[0] + "=" + value[1] + " "
			}
		}
	}
	if app == "" || space == "" {
		return true
	}
	msg.Log = reformattedMessage
	msg.Stream = ""
	msg.Time = time.Now()
	msg.Space = space
	msg.Kubernetes.NamespaceName = space
	msg.Kubernetes.PodId = ""
	msg.Kubernetes.PodName = "akkeris/router"
	msg.Kubernetes.ContainerName = app
	msg.Kubernetes.Labels.Name = ""
	msg.Kubernetes.Labels.PodTemplateHash = ""
	msg.Kubernetes.Host = ""
	msg.Topic = space
	msg.Tag = ""
	return false
}



func ParseBuildLogMessage(bmsg BuildLogSpec) (LogSpec, bool) {
	var app = ""
	var space = ""

	var splitAppName = strings.SplitN(bmsg.Metadata, "-", 2)
	app = splitAppName[0]
	if len(splitAppName) == 1 {
		space = "default"
	} else {
		space = splitAppName[1]
	}
	
	if app == "" || space == "" {
		return LogSpec{}, true
	}
	rex, err := regexp.Compile("(Step \\d+/\\d+ : ARG [0-9A-Za-z_]+=).*")
	if err == nil {
		bmsg.Message = rex.ReplaceAllString(bmsg.Message, "${1}...")
	}
	return LogSpec{
		Log:    bmsg.Message,
		Stream: "",
		Time:   time.Now(),
		Space:  space,
		Docker: DockerSpec{ContainerId: ""},
		Kubernetes: KubernetesSpec{
			NamespaceName: space,
			PodId:         "",
			PodName:       "akkeris/build",
			ContainerName: app,
			Labels: LabelsSpec{
				Name:            "",
				PodTemplateHash: ""},
			Host: ""},
		Topic: space, Tag: ""}, false
}


