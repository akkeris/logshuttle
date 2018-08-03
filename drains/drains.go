package drains

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"../syslog"
)

type Drain struct {
	Url      string
	Packets  chan syslog.Packet
	buffered []syslog.Packet
	frame    int
	pool 	 *Pool
}

var drains []*Drain;
var pools_list []*Pool;
var pools map[string]*Pool;
var pool_mutex *sync.Mutex;
var bad_hosts_mutex *sync.Mutex;
var bad_hosts map[string]bool;

const MaxLogSize int = 99990;

func PrintMetrics() {
	pool_mutex.Lock()
	for _, pool := range pools_list {
		pool.PrintMetrics()
	}
	pool_mutex.Unlock()
}

func Dial(Url string) (*Drain, error) {
	if strings.HasPrefix(Url, "http://") || strings.HasPrefix(Url, "https://") {
		drain := &Drain{
			Url:      Url,
			Packets:  make(chan syslog.Packet),
			buffered: make([]syslog.Packet, 0),
			frame:    0}

		go drain.buffer()

		drains = append(drains, drain)
		return drain, nil
	} else {
		pool_mutex.Lock()
		bad_hosts_mutex.Lock()
		if _, ok := bad_hosts[Url]; ok {
			bad_hosts_mutex.Unlock()
			pool_mutex.Unlock()
			return nil, fmt.Errorf("host already failed to connect.")
		}
		bad_hosts_mutex.Unlock()
		// If we already have a pool for this, go ahead and return.
		if _, ok := pools[Url]; ok {
			d := &Drain{Url: Url,
				Packets: pools[Url].Packets,
				buffered: make([]syslog.Packet, 0),
				frame: 0,
				pool: pools[Url]}
			pool_mutex.Unlock()
			return d, nil
		}
		// otherwise create a new pool.
		pool := &Pool{}
		if err := pool.Init(Url); err != nil {
			bad_hosts_mutex.Lock()
			bad_hosts[Url] = true
			bad_hosts_mutex.Unlock()
			pool_mutex.Unlock()
			fmt.Printf("[drains] Unable to process route (%s): %s\n", Url, err)
			return nil, err
		}

		pools[Url] = pool
		pools_list = append(pools_list, pool)
		pool_mutex.Unlock()
		drain := &Drain{Url: Url,
			Packets: pool.Packets,
			buffered: make([]syslog.Packet, 0),
			frame: 0,
			pool: pool}
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
		res, err := client.Do(req)
		if err != nil {
			res.Body.Close()
		}
	} else {
		fmt.Printf("[drains] Error getting a drain: %s", err);
	}
}

func (l *Drain) buffer() {
	for p := range l.Packets {
		l.buffered = append(l.buffered, p)
		if len(l.buffered) >= 500 {
			l.Drain()
		}
	}
}

func CloseSyslogDrains() {
	for _, p := range pools {
		p.Close()
	}
}

func InitSyslogDrains() {
	pool_mutex = &sync.Mutex{}
	bad_hosts_mutex = &sync.Mutex{}
	pools = make(map[string]*Pool)
	pools_list = make([]*Pool, 0)
	bad_hosts = make(map[string]bool)
	ticker := time.NewTicker(time.Second * 60 * 5)
	go func() {
		for {
			bad_hosts_mutex.Lock()
			bad_hosts = make(map[string]bool)
			bad_hosts_mutex.Unlock()
			<-ticker.C
		}
	}()
}

func InitUrlDrains() {
	drains := make([]*Drain, 0)
	ticker := time.NewTicker(time.Second * 3)
	go func() {
		for {
			for _, d := range drains {
				d.Drain()
			}
			<-ticker.C
		}
	}()
}
