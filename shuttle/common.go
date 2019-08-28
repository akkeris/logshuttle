package shuttle

import (
	"encoding/json"
	"github.com/akkeris/logshuttle/events"
	"net/url"
	"regexp"
	"strings"
	"time"
	"strconv"
)

func ContainerToProc(container string) events.Process {
	proc := events.Process{App: container, Type: "web"}
	if strings.Index(container, "--") != -1 {
		var components = strings.SplitN(container, "--", 2)
		proc = events.Process{App: components[0], Type: components[1]}
	}
	return proc
}

func IsAppMatch(potential string, app_name string) bool {
	return potential == app_name || strings.HasPrefix(potential, app_name+"--")
}

type istioLogSpec struct {
	Time      time.Time `json:"time"`
	Severity  string    `json:"severity"`
	Bytes     int       `json:"bytes"`
	Method    string    `json:"method"`
	Source    string    `json:"source"`
	Space     string    `json:"space"`
	Path      string    `json:"path"`
	RequestId string    `json:"request_id"`
	Origin	  string	`json:"origin,omitempty"`
	From      string    `json:"from"`
	Host      string    `json:"host"`
	App       string    `json:"app"`
	Fwd       string    `json:"fwd"`
	Status    int       `json:"status"`
	Service   string    `json:"service"`
	Dyno      string    `json:"dyno"`
	Total     string    `json:"total"`
}

type buildLogSpec struct {
	Metadata string `json:"metadata"`
	Build    int    `json:"build"`
	Job      string `json:"job"`
	Message  string `json:"message"`
}

func ParseIstioWebLogMessage(data []byte, msg *events.LogSpec) bool {
	var istioMsg istioLogSpec
	if err := json.Unmarshal(data, &istioMsg); err != nil {
		return true
	}

	msg.Log = "bytes=" + strconv.Itoa(istioMsg.Bytes) + " " +
		"method=" + istioMsg.Method + " " +
		"request_id=" + istioMsg.RequestId + " " +
		"fwd=" + istioMsg.Fwd + " " +
		"origin=" + istioMsg.Origin + " " +
		"status=" + strconv.Itoa(istioMsg.Status) + " " +
		"service=" + istioMsg.Service + " " +
		"total=" + istioMsg.Total + " " +
		"source=" + istioMsg.Source + " " +
		"dyno=" + istioMsg.Dyno

	msg.Stream = ""
	msg.Time = time.Now()
	msg.Space = istioMsg.Space
	msg.Site = "" // TODO
	msg.SitePath = "" // TODO
	msg.Path = istioMsg.Path
	msg.Kubernetes.NamespaceName = istioMsg.Space
	msg.Kubernetes.PodId = ""
	msg.Kubernetes.PodName = "akkeris/router"
	msg.Kubernetes.ContainerName = istioMsg.App
	msg.Kubernetes.Labels.Name = ""
	msg.Kubernetes.Labels.PodTemplateHash = ""
	msg.Kubernetes.Host = ""
	msg.Topic = istioMsg.Space
	msg.Tag = ""

	return false
}

func ParseBuildLogMessage(data []byte, msg *events.LogSpec) bool {
	var bmsg buildLogSpec
	if err := json.Unmarshal(data, &bmsg); err != nil {
		return true
	} else {
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
			return true
		}

		rex, err := regexp.Compile("(Step \\d+/\\d+ : ARG [0-9A-Za-z_]+=).*")
		if err == nil {
			bmsg.Message = rex.ReplaceAllString(bmsg.Message, "${1}...")
		}

		msg.Log = bmsg.Message
		msg.Stream = ""
		msg.Time = time.Now()
		msg.Space = space
		msg.Kubernetes.NamespaceName = space
		msg.Kubernetes.PodId = ""
		msg.Kubernetes.PodName = "akkeris/build"
		msg.Kubernetes.ContainerName = app
		msg.Kubernetes.Labels.Name = ""
		msg.Kubernetes.Labels.PodTemplateHash = ""
		msg.Kubernetes.Host = ""
		msg.Topic = space
		msg.Tag = ""
		return false
	}
}

func ParseWebLogMessage(data []byte, msg *events.LogSpec) bool {
	var app = ""
	var space = ""
	var reformattedMessage = ""
	var site = ""
	var site_path = ""
	var path = ""
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
			} else if value[0] == "source" {
				unescaped, err := url.QueryUnescape(strings.TrimSpace(value[1]))
				if err != nil {
					reformattedMessage = reformattedMessage + "fwd=\"" + value[1] + "\" "
				} else {
					reformattedMessage = reformattedMessage + "fwd=\"" + unescaped + "\" "
				}
			} else if value[0] == "path" {
				path = value[1]
			} else if value[0] == "site_domain" {
				site = value[1]
			} else if value[0] == "site_path" {
				site_path = value[1]
			} else if value[0] != "timestamp" {
				reformattedMessage = reformattedMessage + value[0] + "=" + value[1] + " "
			}
		}
	}
	if app == "" || space == "" {
		return true
	}
	msg.Log = strings.TrimSpace(reformattedMessage)
	msg.Stream = ""
	msg.Time = time.Now()
	msg.Space = space
	msg.Site = site
	msg.SitePath = site_path
	msg.Path = path
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
