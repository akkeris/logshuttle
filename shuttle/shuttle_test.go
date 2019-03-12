package shuttle

import (
	"encoding/json"
	"fmt"
	kafka "github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/go-martini/martini"
	. "github.com/smartystreets/goconvey/convey"
	"gopkg.in/mcuadros/go-syslog.v2"
	"io/ioutil"
	"log"
	"logshuttle/drains"
	"logshuttle/events"
	"logshuttle/storage"
	syslog2 "logshuttle/syslog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func CreateMemoryStorage() *storage.Storage {
	var store storage.MemoryStorage
	store.Init("")
	var s storage.Storage = &store
	return &s
}

func CreateShuttle(s *storage.Storage) Shuttle {
	drains.Init()

	var shuttle Shuttle
	shuttle.Init(s, []string{}, "gotest")
	return shuttle
}

func CreateUDPSyslogServer() syslog.LogPartsChannel {
	channel := make(syslog.LogPartsChannel)
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.RFC5424)
	server.SetHandler(handler)
	server.ListenUDP("0.0.0.0:11514")
	server.Boot()
	go server.Wait()
	return channel
}

func CreateTCPSyslogServer(port string) syslog.LogPartsChannel {
	channel := make(syslog.LogPartsChannel)
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.RFC5424)
	server.SetHandler(handler)
	server.ListenTCP("0.0.0.0:" + port)
	server.Boot()
	go server.Wait()
	return channel
}

func CreateHTTPTestChannel(channel syslog.LogPartsChannel) func(http.ResponseWriter, *http.Request) {
	return func(res http.ResponseWriter, req *http.Request) {
		bytes, err := ioutil.ReadAll(req.Body)
		if err != nil {
			log.Fatalln(err)
		}
		lines := strings.Split(string(bytes), "\n")
		for _, line := range lines {
			if line != "" {
				ind := strings.Index(line, " ") + 1
				line := line[ind:len(line)]
				parser := syslog.RFC5424.GetParser(([]byte)(line))
				err := parser.Parse()
				if err != nil {
					log.Fatalln(err)
				}
				logParts := parser.Dump()
				channel <- logParts
			}
		}
		req.Body.Close()
		res.Write(([]byte)("OK"))
	}
}

func CreateHTTPSyslogServer() syslog.LogPartsChannel {
	channel := make(syslog.LogPartsChannel)
	m := martini.Classic()
	m.Post("/tests", CreateHTTPTestChannel(channel))
	go m.RunOnAddr(":3333")
	return channel
}

func CreateAppMessage(shuttle Shuttle, app string, space string, message string, stream string) {
	var e events.LogSpec
	e.Topic = space
	e.Kubernetes.ContainerName = app
	e.Kubernetes.PodName = "1234-web-abc"
	e.Time = time.Now()
	e.Log = message
	e.Stream = stream
	bytes, _ := json.Marshal(e)
	k := kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &space, Partition: 0}, Timestamp: time.Now(), Key: []byte(""), Value: bytes}
	shuttle.consumer.AppLogs <- &k
}

func CreateHttpMessage(shuttle Shuttle, site string, site_path string, app string, space string, app_path string, method string, source string, extra string) {
	logline := fmt.Sprintf("hostname=%s-%s source=%s path=%s timestamp=%s", app, space, source, app_path, time.Now().UTC().Format(time.RFC3339))
	if site != "" {
		logline += " site_domain=" + site + " site_path=" + site_path
	}
	if extra != "" {
		logline += " " + extra
	}
	var topic = "alamoweblogs"
	k := kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 0}, Timestamp: time.Now(), Key: []byte(""), Value: []byte(logline)}
	shuttle.consumer.WebLogs <- &k
}

func TestShuttle(t *testing.T) {
	mem := CreateMemoryStorage()
	udp := CreateUDPSyslogServer()
	tcp := CreateTCPSyslogServer("11515")
	tcp_site := CreateTCPSyslogServer("11516")
	tcp_site2 := CreateTCPSyslogServer("11517")
	web := CreateHTTPSyslogServer()
	shuttle := CreateShuttle(mem)

	// Add the UDP and TCP syslog listeners
	udp_route := storage.Route{Id: "test", Space: "space", App: "app", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+udp://127.0.0.1:11514"}
	tcp_route := storage.Route{Id: "test2", Space: "space2", App: "app", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+tcp://127.0.0.1:11515"}
	web_route := storage.Route{Id: "test3", Space: "space3", App: "app", Created: time.Now(), Updated: time.Now(), DestinationUrl: "http://localhost:3333/tests"}
	tcp_site_route := storage.Route{Id: "test999", Space: "", App: "", Site: "foobar-hello.com", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+tcp://127.0.0.1:11516"}
	tcp_site_route2 := storage.Route{Id: "test1000", Space: "space55", App: "app55", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+tcp://127.0.0.1:11517"}
	(*mem).AddRoute(udp_route)
	(*mem).AddRoute(tcp_route)
	(*mem).AddRoute(web_route)
	shuttle.Refresh()

	Convey("Ensure we can post and receive a log message via udp syslog (and in order)", t, func() {
		CreateHttpMessage(shuttle, "", "", "app", "space", "/some_path", "post", "1.1.1.1", "")
		logMsg := <-udp
		So(logMsg["message"], ShouldEqual, "fwd=\"1.1.1.1\" host=app-space path=/some_path")
	})

	Convey("Ensure we can post and receive a log message via udp syslog (and in order)", t, func() {
		CreateAppMessage(shuttle, "app", "space", "Oh hello.", "stdout")
		CreateAppMessage(shuttle, "app", "space2", "Oh hello3", "stdout")
		CreateAppMessage(shuttle, "app", "space3", "Oh hello31", "stdout")
		CreateAppMessage(shuttle, "app2", "space", "Oh hello4", "stdout")
		CreateAppMessage(shuttle, "app", "space", "Oh hello2.", "stdout")
		CreateAppMessage(shuttle, "app", "space2", "Oh hello5", "stdout")
		CreateAppMessage(shuttle, "app", "space3", "Oh hello6", "stdout")
		logMsg := <-udp
		So(logMsg["message"], ShouldEqual, "Oh hello.")
		So(logMsg["hostname"], ShouldEqual, "app-space")
		logMsg = <-udp
		So(logMsg["message"], ShouldEqual, "Oh hello2.")
		So(logMsg["hostname"], ShouldEqual, "app-space")
	})

	Convey("Ensure we can post and receive a log message via tcp syslog (and in order)", t, func() {
		logMsg := <-tcp
		So(logMsg["message"], ShouldEqual, "Oh hello3")
		So(logMsg["hostname"], ShouldEqual, "app-space2")
		logMsg = <-tcp
		So(logMsg["message"], ShouldEqual, "Oh hello5")
		So(logMsg["hostname"], ShouldEqual, "app-space2")
	})

	Convey("Ensure we can post and receive a log message via http syslog (and in order)", t, func() {
		logMsg := <-web
		So(logMsg["message"], ShouldEqual, "Oh hello31")
		So(logMsg["hostname"], ShouldEqual, "app-space3")
		logMsg = <-web
		So(logMsg["message"], ShouldEqual, "Oh hello6")
		So(logMsg["hostname"], ShouldEqual, "app-space3")
	})

	Convey("Ensure refresh doesnt botch the routes", t, func() {
		shuttle.Refresh()
		So(len(shuttle.routes), ShouldEqual, 3)
	})

	Convey("Ensure adding a bad route doesnt get acknowledged.", t, func() {
		(*mem).AddRoute(storage.Route{Id: "test5", Space: "space", App: "app3", Created: time.Now(), Updated: time.Now(), DestinationUrl: "this is not a destination url.."})
		shuttle.Refresh()
		(*mem).AddRoute(storage.Route{Id: "test6", Space: "space", App: "app4", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+tcp://10.243.243.243:10"})
		shuttle.Refresh()
		So(len(shuttle.routes), ShouldEqual, 3)
		So(len(shuttle.routes["appspace"]), ShouldEqual, 1)
		So(len(shuttle.routes["appspace2"]), ShouldEqual, 1)
	})

	Convey("Ensure failure of bad routes did not prevent messages from routing.", t, func() {
		CreateAppMessage(shuttle, "app", "space", "oh boy", "stdout")
		CreateAppMessage(shuttle, "app", "space2", "oh girl", "stdout")
		logMsg := <-tcp
		So(logMsg["message"], ShouldEqual, "oh girl")
		So(logMsg["hostname"], ShouldEqual, "app-space2")
		logMsg = <-udp
		So(logMsg["message"], ShouldEqual, "oh boy")
		So(logMsg["hostname"], ShouldEqual, "app-space")
	})

	Convey("Ensure we specify an error severity if sent from stderr.", t, func() {
		CreateAppMessage(shuttle, "app", "space2", "oh error", "stderr")
		logMsg := <-tcp
		So(logMsg["message"], ShouldEqual, "oh error")
		So(logMsg["hostname"], ShouldEqual, "app-space2")
		So(logMsg["severity"], ShouldEqual, syslog2.SevErr)
	})

	Convey("Ensure route is removed from logshuttle when changed in storage", t, func() {
		count, err := drains.DrainCount(udp_route.DestinationUrl)
		So(err, ShouldEqual, nil)
		So(count, ShouldEqual, 1)
		err = (*mem).RemoveRoute(udp_route)
		So(err, ShouldEqual, nil)
		shuttle.Refresh()
		routes, ok := shuttle.routes["appspace"]
		So(ok, ShouldEqual, true)
		So(len(routes), ShouldEqual, 0)
		CreateAppMessage(shuttle, "app", "space", "Oh hello.", "stdout")
		So(len(udp), ShouldEqual, 0)
		count, err = drains.DrainCount(udp_route.DestinationUrl)
		So(err, ShouldEqual, nil)
		So(count, ShouldEqual, 0)
	})

	Convey("Ensure two routes with the same drain, removing one route doesnt remove the drain", t, func() {
		route1 := storage.Route{Id: "test55", Space: "space5", App: "app1", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+tcp://127.0.0.1:11515"}
		route2 := storage.Route{Id: "test66", Space: "space6", App: "app2", Created: time.Now(), Updated: time.Now(), DestinationUrl: "syslog+tcp://127.0.0.1:11515"}
		(*mem).AddRoute(route1)
		(*mem).AddRoute(route2)
		shuttle.Refresh()
		count, err := drains.DrainCount("syslog+tcp://127.0.0.1:11515")
		So(err, ShouldEqual, nil)
		So(count, ShouldEqual, 3)
		(*mem).RemoveRoute(route1)
		shuttle.Refresh()
		count, err = drains.DrainCount("syslog+tcp://127.0.0.1:11515")
		So(err, ShouldEqual, nil)
		So(count, ShouldEqual, 2)
	})

	Convey("Ensure we can receive site routes, and both app and site routes are received.", t, func() {
		(*mem).AddRoute(tcp_site_route)
		(*mem).AddRoute(tcp_site_route2)
		shuttle.Refresh()
		//
		CreateHttpMessage(shuttle, "foobar-hello.com", "/other_path", "app55", "space55", "/some_path", "post", "1.1.1.1", "")
		logMsg := <-tcp_site2
		So(logMsg["message"], ShouldEqual, "fwd=\"1.1.1.1\" host=app55-space55 path=/some_path")
		logMsg = <-tcp_site
		So(logMsg["message"], ShouldEqual, "fwd=\"1.1.1.1\" host=foobar-hello.com path=/other_path")
	})
}
