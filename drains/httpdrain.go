package drains

import (
	"log"
	"sync"
	"net/http"
	"net/url"
	"time"
	"../syslog"
	"strconv"
	"bytes"
)

type HttpDrain struct {
	id 		 string
	url      string
	packets  chan syslog.Packet
	buffered []syslog.Packet
	frame    int
	trans    *http.Transport
	client   *http.Client
	mutex 	 *sync.Mutex
	sent     int
	conns    int
	errors   int
	pretty_url *url.URL
	pressure float64
	draining bool
	closed   bool
}

func (l *HttpDrain) Init(Id string, Url string) (error) {
	log.Printf("[drains] Creating URL drain to %s\n", Url)
	l.id = Id
	l.url = Url
	u, err := url.Parse(Url)
	if err != nil {
		return err
	}
	l.packets = make(chan syslog.Packet)
	l.buffered = make([]syslog.Packet, 0)
	l.frame = 0
	l.client = &http.Client{Transport: &http.Transport{MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}}
	l.mutex = &sync.Mutex{}
	l.pretty_url = u
	l.pressure = float64(0)
	l.sent = 0
	l.conns = 0
	l.draining = false
	l.closed = false
	l.errors = 0
	go l.writeLoop()
	return nil
}

func (l *HttpDrain) Packets() (chan syslog.Packet) {
	return l.packets
}

func (l *HttpDrain) Close() {
	l.closed = true
}

func (l *HttpDrain) Url() string {
	return l.url
}

func (l *HttpDrain) Id() string {
	return l.id
}

func (l *HttpDrain) PrintMetrics() {
	log.Printf("[metrics] syslog=%s://%s%s max#connections=-1 count#errors=%d count#connections=%d measure#pressure=%f%% count#sent=%d\n", l.pretty_url.Scheme, l.pretty_url.Host, l.pretty_url.Path, l.errors, l.conns, l.pressure * 100, l.sent)
	l.sent = 0
	l.conns = 0
	l.errors = 0
}

func (l *HttpDrain) Flush() {
	if l.draining == true || l.closed == true {
		return
	}

	l.mutex.Lock()
	l.draining = true
	defer l.mutex.Unlock()

	body := ""
	size := 0
	l.pressure = float64(len(l.buffered))/float64(1024)
	if len(l.buffered) == 0 {
		l.draining = false
		return
	}
	for _, p := range l.buffered {
		size++
		l.sent++
		t := p.Generate(1024 * 4)
		body += strconv.Itoa(len(t)+1) + " " + t + "\n"
	}
	l.buffered = make([]syslog.Packet, 0)
	l.conns++
	l.frame++
	req, err := http.NewRequest(http.MethodPost, l.url, bytes.NewBufferString(body))
	if err == nil {
		req.Header.Add("Logplex-Msg-Count", strconv.Itoa(size))
		req.Header.Add("Logplex-Frame-Id", strconv.Itoa(l.frame))
		req.Header.Add("Logplex-Drain-Token", l.id)
		req.Header.Add("User-Agent", "Logplex/v72")
		req.Header.Add("Content-Type", "application/logplex-1")
		res, err := l.client.Do(req)
		if err == nil {
			res.Body.Close()
		}
		if err != nil || res.StatusCode > 399 || res.StatusCode < 200 {
			l.errors++
		}
	} else {
		log.Printf("[drains] Error getting a drain: %s\n", err);
	}
	l.draining = false
}

func (l *HttpDrain) writeLoop() {
	for p := range l.packets {
		if l.closed == true {
			return
		}
		l.mutex.Lock()
		l.buffered = append(l.buffered, p)
		l.mutex.Unlock()
		if len(l.buffered) > 64 {
			go l.Flush()
		}
	}
}
