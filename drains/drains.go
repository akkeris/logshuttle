package drains

import (
	"bytes"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"net/http"
	"time"
	"strconv"
	"strings"
	"fmt"
	"log"
)

type Drain struct {
	Url      string
	Packets  chan syslog.Packet
	Errors   chan error
	buffered []syslog.Packet
	frame    int
	endpoint *syslog.Logger
}

var drains []*Drain;

const MaxLogSize int = 99990;

func Dial(kafkaGroup string, Url string) (*Drain, error) {
	if strings.HasPrefix(Url, "http://") || strings.HasPrefix(Url, "https://") {
		drain := &Drain{
			Url:      Url,
			Packets:  make(chan syslog.Packet, 100),
			Errors:   make(chan error, 0),
			buffered: make([]syslog.Packet, 0),
			frame:    0,
			endpoint: nil}

		go drain.buffer()

		drains = append(drains, drain)
		return drain, nil
	} else {
		var network = "tls"
		var host = Url
		if strings.HasPrefix(Url, "syslog+tcp://") || strings.HasPrefix(Url, "syslog://") || strings.HasPrefix(Url, "tcp://") {
			network = "tcp"
		} else if strings.HasPrefix(Url, "syslog+udp://") || strings.HasPrefix(Url, "udp://") {
			network = "udp"
		} else if strings.HasPrefix(Url, "syslog+tls://") || strings.HasPrefix(Url, "ssh://") {
			network = "tls"
		} else {
			return nil, fmt.Errorf("Warning unknown url schema provided: %s", Url)
		}

		if strings.Contains(Url, "://") {
			host = strings.Split(Url, "://")[1]
		}

		dest, err := syslog.Dial(kafkaGroup, network, host, nil, time.Second*2, time.Second*4, MaxLogSize)
		if err != nil {
			fmt.Println(err)
			return nil, fmt.Errorf("Unable to establish connection for: %s using %s to %s", Url, network, host)
		}
		if dest == nil {
			return nil, fmt.Errorf("Unable to establish connection, no known error %s", host)
		}

		drain := &Drain{Url: Url,
			Packets: dest.Packets,
			Errors: dest.Errors,
			buffered: make([]syslog.Packet, 0),
			frame: 0,
			endpoint: dest}
		return drain, nil
	}
}

func (l *Drain) Drain() {
	body := ""
	size := 0

	for _, p := range l.buffered {
		size++
		t := p.Generate(1024 * 4)
		body += strconv.Itoa(len(t)+1) + " " + t + "\n"
	}

	l.buffered = make([]syslog.Packet, 0)
	l.frame++
	tr := &http.Transport{MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest(http.MethodPost, l.Url, bytes.NewBufferString(body))
	if err == nil {
		req.Header.Add("Logplex-Msg-Count", strconv.Itoa(size))
		req.Header.Add("Logplex-Frame-Id", strconv.Itoa(l.frame))
		req.Header.Add("User-Agent", "Logplex/v72")
		req.Header.Add("Content-Type", "application/logplex-1")
		client.Do(req)
	} else {
		log.Printf("Error getting a drain: %s", err);
	}
}

func (l *Drain) buffer() {
	for p := range l.Packets {
		l.buffered = append(l.buffered, p)
		if len(l.buffered) >= 500 {
			l.Drain();
		}
	}
}

func InitUrlDrains() {
	drains := make([]*Drain, 0)
	ticker := time.NewTicker(time.Second * 3)
	for {
		for _, d := range drains {
			d.Drain()
		}
		<-ticker.C
	}
}
