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
	producer *events.LogProducer
	server *grpc.Server
}

var _ v2.AccessLogServiceServer = &EnvoyAlsServer{}

func New(kafkaAddrs []string, kafkaGroup string, port string) (v2.AccessLogServiceServer, error) {
	s := &EnvoyAlsServer{
		producer: &events.LogProducer{},
	}
	err := s.producer.Init(kafkaAddrs, kafkaGroup)
	if err != nil {
		return s, err
	}
	s.server = grpc.NewServer()
	v2.RegisterAccessLogServiceServer(s.server, s)
	l, err := net.Listen("tcp", ":" + port)
	if err != nil {
		return s, err
	}
	s.server.Serve(l)
	return s, nil
}

func (s *EnvoyAlsServer) Close() {
	s.producer.Close()
	s.server.Stop()
}

func (s *EnvoyAlsServer) StreamAccessLogs(stream v2.AccessLogService_StreamAccessLogsServer) error {
	log.Println("Started envoy access log stream")
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		str, _ := s.marshaler.MarshalToString(in)
		return s.producer.AddRaw("istio-access-logs", str)
	}
}

func (eas *EnvoyAlsServer) StartEnvoyALSAdapter(port int, kafkaAddrs []string, kafkaGroup string) error {
	_, err := New(kafkaAddrs, kafkaGroup, strconv.Itoa(port))
	if err != nil {
		return err
	}
	log.Println("Listening on tcp://localhost:" + strconv.Itoa(port) + " for envoy access logs")
	return nil
}