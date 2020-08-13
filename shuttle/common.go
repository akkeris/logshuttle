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

type ResponseFlags struct {
	FailedLocalHealthcheck          bool                        `json:"failed_local_healthcheck,omitempty"`
	NoHealthyUpstream               bool                        `json:"no_healthy_upstream,omitempty"`
	UpstreamRequestTimeout          bool                        `json:"upstream_request_timeout,omitempty"`
	LocalReset                      bool                        `json:"local_reset,omitempty"`
	UpstreamRemoteReset             bool                        `json:"upstream_remote_reset,omitempty"`
	UpstreamConnectionFailure       bool                        `json:"upstream_connection_failure,omitempty"`
	UpstreamConnectionTermination   bool                        `json:"upstream_connection_termination,omitempty"`
	UpstreamOverflow                bool                        `json:"upstream_overflow,omitempty"`
	NoRouteFound                    bool                        `json:"no_route_found,omitempty"`
	DelayInjected                   bool                        `json:"delay_injected,omitempty"`
	FaultInjected                   bool                        `json:"fault_injected,omitempty"`
	RateLimited                     bool                        `json:"rate_limited,omitempty"`
	RateLimitServiceError           bool                        `json:"rate_limit_service_error,omitempty"`
	DownstreamConnectionTermination bool                        `json:"downstream_connection_termination,omitempty"`
	UpstreamRetryLimitExceeded      bool                        `json:"upstream_retry_limit_exceeded,omitempty"`
	StreamIdleTimeout               bool                        `json:"stream_idle_timeout,omitempty"`
	InvalidEnvoyRequestHeaders      bool                        `json:"invalid_envoy_request_headers,omitempty"`
	DownstreamProtocolError         bool                        `json:"downstream_protocol_error,omitempty"`
}
type TLSProperties struct {
	TLSVersion *string `json:"tls_version,omitempty"`
	TLSSNIHostname *string `json:"tls_sni_hostname,omitempty"`
}
type CommonProperties struct {
	StartTime	*time.Time 	`json:"start_time"`
	TimeToLastUpstreamTxByte	*string 	`json:"time_to_last_upstream_tx_byte"`
	TimeToLastRxByte 			*string 	`json:"time_to_last_rx_byte"`
	UpstreamCluster	string `json:"upstream_cluster"`
	ResponseFlags *ResponseFlags `json:"response_flags,omitempty"`
	TLSProperties *TLSProperties `json:"tls_properties,omitempty"`
}
type Request struct {
	RequestMethod string `json:"request_method"`
	Authority string `json:"authority"`
	Path string `json:"path"`
	UserAgent string `json:"user_agent"`
	ForwardedFor string `json:"forwarded_for"`
	RequestId string `json:"request_id"`
	OriginalPath string `json:"original_path,omitempty"`
	RequestHeadersBytes string `json:"request_headers_bytes"`
	RequestBodyBytes *string `json:"request_body_bytes,omitempty"`
}
type Response struct {
	ResponseCode *uint32 `json:"response_code"`
	ResponseHeadersBytes string `json:"response_headers_bytes"`
	ResponseBodyBytes *string `json:"response_body_bytes,omitempty"`
}
type istio16LogSpec struct {
	CommonProperties *CommonProperties `json:"common_properties"`
	ProtocolVersion string `json:"protocol_version"`
	Request *Request  `json:"request"`
	Response *Response `json:"response"`
}

type buildLogSpec struct {
	Metadata string `json:"metadata"`
	Build    int    `json:"build"`
	Job      string `json:"job"`
	Message  string `json:"message"`
}

func StringToIntOrZero(target *string) int {
	if target == nil {
		return 0
	}
	val, err := strconv.Atoi(*target)
	if err != nil {
		return 0
	}
	return val
}
func ParseIstioFromEnvoyWebLogMessage(data []byte, msg *events.LogSpec) bool {
	//https://github.com/envoyproxy/go-control-plane/blob/master/envoy/data/accesslog/v2/accesslog.pb.go#L246
	//https://github.com/envoyproxy/go-control-plane/blob/master/envoy/data/accesslog/v2/accesslog.pb.go#L857
	//https://github.com/envoyproxy/go-control-plane/blob/master/envoy/data/accesslog/v2/accesslog.pb.go#L372

	var istioMsg istio16LogSpec
	if err := json.Unmarshal(data, &istioMsg); err != nil {
		return true
	}
	if istioMsg.CommonProperties == nil ||
		istioMsg.CommonProperties.UpstreamCluster == "" || 
		istioMsg.Response == nil ||
		istioMsg.Request == nil ||
		istioMsg.Response.ResponseCode == nil || 
		istioMsg.CommonProperties.TimeToLastUpstreamTxByte == nil || 
		istioMsg.CommonProperties.TimeToLastRxByte == nil {
		return true
	}

	var code int = 0
    if istioMsg.CommonProperties.ResponseFlags != nil && istioMsg.CommonProperties.ResponseFlags.DownstreamConnectionTermination {
		code = 499
	}
	if istioMsg.Response.ResponseCode != nil {
		code = int(*istioMsg.Response.ResponseCode)
	}

	var tlsVersion string = ""
	if istioMsg.CommonProperties.TLSProperties != nil && istioMsg.CommonProperties.TLSProperties.TLSVersion != nil {
		tlsVersion = *istioMsg.CommonProperties.TLSProperties.TLSVersion
	}

	var tlsSNIHostname string = ""
	if istioMsg.CommonProperties.TLSProperties != nil && istioMsg.CommonProperties.TLSProperties.TLSSNIHostname != nil {
		tlsSNIHostname = "https://" + (*istioMsg.CommonProperties.TLSProperties.TLSSNIHostname) + istioMsg.Request.OriginalPath
	}


	c := strings.Split(istioMsg.CommonProperties.UpstreamCluster, "|")
	d := strings.Split(c[3], ".")
	app := d[0]
	space := d[1]
	msg.Log = "bytes=" + strconv.Itoa(int(StringToIntOrZero(&istioMsg.Request.RequestHeadersBytes) + StringToIntOrZero(istioMsg.Request.RequestBodyBytes) + StringToIntOrZero(&istioMsg.Response.ResponseHeadersBytes) + StringToIntOrZero(istioMsg.Response.ResponseBodyBytes))) + " " +
		"request_size=" + strconv.Itoa(int(StringToIntOrZero(&istioMsg.Request.RequestHeadersBytes) + StringToIntOrZero(istioMsg.Request.RequestBodyBytes))) + " " +
		"response_size=" + strconv.Itoa(int(StringToIntOrZero(&istioMsg.Response.ResponseHeadersBytes) + StringToIntOrZero(istioMsg.Response.ResponseBodyBytes))) + " " +
		"method=" + string(istioMsg.Request.RequestMethod) + " " +
		"request_id=" + istioMsg.Request.RequestId + " " +
		"fwd=" + istioMsg.Request.ForwardedFor + " " +
		"authority=" + istioMsg.Request.Authority + " " +
		"origin=" + tlsSNIHostname + " " +
		"protocol=" + strings.ToLower(istioMsg.ProtocolVersion) + " " +
		"tls=" + tlsVersion + " " +
		"status=" + strconv.Itoa(code) + " " +
		"service=" + *istioMsg.CommonProperties.TimeToLastUpstreamTxByte + " " +
		"total=" +  *istioMsg.CommonProperties.TimeToLastRxByte + " " +
		"dyno=" + app + "-" + space

	msg.Stream = ""
	msg.Time = time.Now()
	msg.Space = space
	msg.Site = "" // TODO
	msg.SitePath = "" // TODO
	msg.Path = istioMsg.Request.Path
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


var rex *regexp.Regexp

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
		if rex == nil {
			rex = regexp.MustCompile("(Step \\d+/\\d+ : ARG [0-9A-Za-z_]+=).*")
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
