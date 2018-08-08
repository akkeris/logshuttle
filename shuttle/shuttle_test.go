package shuttle

import (
	syslog2 "../syslog"
	"../storage"
	"../drains"
	"../events"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/go-martini/martini"
	kafka "github.com/confluentinc/confluent-kafka-go/kafka"
	"gopkg.in/mcuadros/go-syslog.v2"
	"encoding/json"
	"testing"
	"time"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

func CreateMemoryStorage() (*storage.Storage) {
	var store storage.MemoryStorage
	store.Init("")
	var s storage.Storage = &store
	return &s
}

func CreateShuttle(s *storage.Storage) (Shuttle) {
	drains.Init()

	var shuttle Shuttle
	shuttle.Init(s, []string{}, "gotest")
	return shuttle
}

func CreateUDPSyslogServer() (syslog.LogPartsChannel) {
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

func CreateTCPSyslogServer() (syslog.LogPartsChannel) {
	channel := make(syslog.LogPartsChannel)
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.RFC5424)
	server.SetHandler(handler)
	server.ListenTCP("0.0.0.0:11515")
	server.Boot()
	go server.Wait()
	return channel
}


func CreateHTTPTestChannel(channel syslog.LogPartsChannel)  func(http.ResponseWriter, *http.Request) {
	return func(res http.ResponseWriter, req *http.Request) {
		bytes, err := ioutil.ReadAll(req.Body)
		if err != nil {
			log.Fatalln(err)
		}
		lines := strings.Split(string(bytes), "\n")
		for _, line := range lines {
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
		req.Body.Close()
		res.Write(([]byte)("OK"))
	}
}

func CreateHTTPSyslogServer() (syslog.LogPartsChannel) {
	channel := make(syslog.LogPartsChannel)
	m := martini.Classic()
	m.Post("/tests",  CreateHTTPTestChannel(channel))
	go m.RunOnAddr(":3333")
	return channel
}

func CreateMessage(shuttle Shuttle, app string, space string, message string, stream string) {
	var e events.LogSpec
	e.Topic = space
	e.Kubernetes.ContainerName = app
	e.Kubernetes.PodName = "1234-web-abc"
	e.Time = time.Now()
	e.Log = message
	e.Stream = stream

	bytes, _ := json.Marshal(e)
	
	k := kafka.Message{TopicPartition: kafka.TopicPartition{Topic:&space, Partition: 0}, Timestamp: time.Now(), Key:[]byte(""), Value:bytes}
	shuttle.consumer.AppLogs <- &k

}

func TestShuttle(t *testing.T) {
	mem := CreateMemoryStorage()
	udp := CreateUDPSyslogServer()
	tcp := CreateTCPSyslogServer()
	web := CreateHTTPSyslogServer()
	shuttle := CreateShuttle(mem)

	// Add the UDP and TCP syslog listeners
	udp_route := storage.Route{Id:"test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"syslog+udp://127.0.0.1:11514"}
	tcp_route := storage.Route{Id:"test2", Space:"space2", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"syslog+tcp://127.0.0.1:11515"}
	web_route := storage.Route{Id:"test3", Space:"space3", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"http://localhost:3333/tests"}
	(*mem).AddRoute(udp_route)
	(*mem).AddRoute(tcp_route)
	(*mem).AddRoute(web_route)
	shuttle.Refresh()

	// Create some fake messages to listen to.
	CreateMessage(shuttle, "app", "space", "Oh hello.", "stdout")
	CreateMessage(shuttle, "app", "space2", "Oh hello3", "stdout")
	CreateMessage(shuttle, "app", "space3", "Oh hello31", "stdout")
	CreateMessage(shuttle, "app2", "space", "Oh hello4", "stdout")
	CreateMessage(shuttle, "app", "space", "Oh hello2.", "stdout")
	CreateMessage(shuttle, "app", "space2", "Oh hello5", "stdout")
	CreateMessage(shuttle, "app", "space3", "Oh hello6", "stdout")

	Convey("Ensure we can post and receive a log message via udp syslog (and in order)", t, func() {
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
		So(len(shuttle.routes), ShouldEqual, 2)
	})

	Convey("Ensure adding a bad route doesnt get acknowledged.", t, func() {
		(*mem).AddRoute(storage.Route{Id:"test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"this is not a destination url.."})
		shuttle.Refresh()
		(*mem).AddRoute(storage.Route{Id:"test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"syslog+tcp://10.243.243.243:10"})
		shuttle.Refresh()
		So(len(shuttle.routes), ShouldEqual, 2)
		So(len(shuttle.routes["appspace"]), ShouldEqual, 1)
		So(len(shuttle.routes["appspace2"]), ShouldEqual, 1)
	})

	Convey("Ensure failure of bad routes did not prevent messages from routing.", t, func() {
		CreateMessage(shuttle, "app", "space", "oh boy", "stdout")
		CreateMessage(shuttle, "app", "space2", "oh girl", "stdout")
		logMsg := <-tcp
		So(logMsg["message"], ShouldEqual, "oh girl")
		So(logMsg["hostname"], ShouldEqual, "app-space2")
		logMsg = <-udp
		So(logMsg["message"], ShouldEqual, "oh boy")
		So(logMsg["hostname"], ShouldEqual, "app-space")
	});

	Convey("Ensure we specify an error severity if sent from stderr.", t, func() {
		CreateMessage(shuttle, "app", "space2", "oh error", "stderr")
		logMsg := <-tcp
		So(logMsg["message"], ShouldEqual, "oh error")
		So(logMsg["hostname"], ShouldEqual, "app-space2")
		So(logMsg["severity"], ShouldEqual, syslog2.SevErr)
	})
}
