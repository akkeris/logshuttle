package shuttle

import (
	events "github.com/akkeris/logshuttle/events"
	"io"
	"log"
	"net"
	"strconv"
	v2 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	"google.golang.org/grpc"
	"github.com/golang/protobuf/jsonpb"
)

type EnvoyAlsServer struct {
	marshaler jsonpb.Marshaler
	producer events.LogProducer
	server *grpc.Server
}

var _ v2.AccessLogServiceServer = &EnvoyAlsServer{}

func (s *EnvoyAlsServer) Close() {
	log.Println("Shutting down als adapter")
	s.server.Stop()
}

func (s *EnvoyAlsServer) StreamAccessLogs(stream v2.AccessLogService_StreamAccessLogsServer) error {
	s.marshaler.OrigName = true
	log.Println("Started envoy access log stream")
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.Printf("Failed to recieve istio access logs: %s\n", err.Error())
			return err
		}
		switch entries := in.LogEntries.(type) {
		case *v2.StreamAccessLogsMessage_HttpLogs:
			for _, entry := range entries.HttpLogs.LogEntry {
				str, _ := s.marshaler.MarshalToString(entry)
				if err := s.producer.AddRaw("istio-access-logs", str); err != nil {
					log.Printf("Failed to send istio access logs to kafka: %s\n", err.Error())
					return err
				}
			}
		}
	}
}

func (s *EnvoyAlsServer) StartEnvoyALSAdapter(port int, producer events.LogProducer) {
	s.producer = producer
	s.server = grpc.NewServer()
	v2.RegisterAccessLogServiceServer(s.server, s)
	l, err := net.Listen("tcp", ":" + strconv.Itoa(port))
	if err != nil {
		log.Fatalf("Unable to start envoy als adapter, listener failed: %s\n", err.Error())
	}
	log.Println("Listening on tcp://localhost:" + strconv.Itoa(port) + " for envoy access logs")

	// Below line blocks.
	s.server.Serve(l)
}