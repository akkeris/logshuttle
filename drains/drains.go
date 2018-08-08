package drains

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"../syslog"
)

type Drain interface {
	Init(Id string, Url string) (error)
	Flush()
	PrintMetrics()
	Packets() (chan syslog.Packet)
	Url() string
	Close()
}

var drains_mutex *sync.Mutex;
var drains map[string]Drain;
var drain_keys []string;
var bad_hosts_mutex *sync.Mutex;
var bad_hosts map[string]bool;

const MaxLogSize int = 99990;

func PrintMetrics() {
	drains_mutex.Lock()
	for _, drain := range drain_keys {
		drains[drain].PrintMetrics()
	}
	drains_mutex.Unlock()
}

func Dial(Id string, Url string) (Drain, error) {
	if Url == "" {
		return nil, fmt.Errorf("Invalid host provided")
	}

	bad_hosts_mutex.Lock()
	if _, ok := bad_hosts[Url]; ok {
		bad_hosts_mutex.Unlock()
		return nil, fmt.Errorf("Host failed to properly connect")
	}
	bad_hosts_mutex.Unlock()

	// If we already have a pool for this, go ahead and return.
	if _, ok := drains[Url]; ok {
		return drains[Url], nil
	}
	drains_mutex.Lock()
	defer drains_mutex.Unlock()

	var drain Drain = nil
	if strings.HasPrefix(Url, "http://") || strings.HasPrefix(Url, "https://") {
		drain = &HttpDrain{}
	} else if strings.HasPrefix(Url, "syslog://") || strings.HasPrefix(Url, "syslog+tcp://") || strings.HasPrefix(Url, "syslog+udp://") || strings.HasPrefix(Url, "syslog+tls://") || strings.HasPrefix(Url, "ssh://") {
		drain = &SyslogDrain{}
	} else {
		return nil, fmt.Errorf("The specified schema format is invalid or not supported")
	}

	if err := drain.Init(Id, Url); err != nil {
		bad_hosts_mutex.Lock()
		bad_hosts[Url] = true
		bad_hosts_mutex.Unlock()
		return nil, err
	}
	drains[Url] = drain
	drain_keys = append(drain_keys, Url)
	return drain, nil
}

func Close() {
	for _, drain := range drain_keys {
		drains[drain].Close()
	}
}

func Init() {
	drains_mutex = &sync.Mutex{}
	bad_hosts_mutex = &sync.Mutex{}
	drains = make(map[string]Drain)
	drain_keys = make([]string, 0)
	bad_hosts = make(map[string]bool)
	// Start bad host check clear
	bad_hosts_ticker := time.NewTicker(time.Second * 60 * 5)
	go func() {
		for {
			bad_hosts_mutex.Lock()
			bad_hosts = make(map[string]bool)
			bad_hosts_mutex.Unlock()
			<-bad_hosts_ticker.C
		}
	}()
	// Start explicit flush ticker
	drain_flush_ticker := time.NewTicker(time.Second * 3)
	go func() {
		for {
			for _, drain := range drain_keys {
				drains[drain].Flush()
			}
			<-drain_flush_ticker.C
		}
	}()
}

