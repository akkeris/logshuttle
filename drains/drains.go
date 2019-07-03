package drains

import (
	"fmt"
	"github.com/akkeris/logshuttle/syslog"
	"strings"
	"sync"
	"time"
)

type Drain interface {
	Init(Id string, Url string) error
	Flush()
	PrintMetrics()
	Packets() chan syslog.Packet
	Id() string
	Url() string
	Close()
}

var drains_mutex *sync.Mutex
var drains map[string]Drain
var drains_count map[string]int
var drain_keys []string
var bad_hosts_mutex *sync.Mutex
var bad_hosts map[string]bool

const MaxLogSize int = 99990

func PrintMetrics() {
	drains_mutex.Lock()
	for _, drain := range drain_keys {
		drains[drain].PrintMetrics()
	}
	drains_mutex.Unlock()
}

func Dial(Id string, Url string) (Drain, error) {
	if Url == "" {
		return nil, fmt.Errorf("The specified route URL was blank.")
	}

	bad_hosts_mutex.Lock()
	if _, ok := bad_hosts[Url]; ok {
		bad_hosts_mutex.Unlock()
		return nil, fmt.Errorf("Host is part of a bad host list.")
	}
	bad_hosts_mutex.Unlock()

	// This lock must remain above this line.
	drains_mutex.Lock()
	defer drains_mutex.Unlock()

	// If we already have a pool for this, go ahead and return.
	if _, ok := drains[Url]; ok {
		drains_count[Url] = drains_count[Url] + 1
		return drains[Url], nil
	}

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
	drains_count[Url] = 1
	drains[Url] = drain
	drain_keys = append(drain_keys, Url)
	return drain, nil
}

func DrainCount(Url string) (int, error) {
	drains_mutex.Lock()
	defer drains_mutex.Unlock()
	if val, ok := drains_count[Url]; ok {
		return val, nil
	} else {
		return 0, fmt.Errorf("Unable to find drain %s", Url)
	}
}

func Undial(Id string, Url string) error {
	drains_mutex.Lock()
	defer drains_mutex.Unlock()
	if val, ok := drains_count[Url]; ok {
		drains_count[Url] = val - 1
		// we should erase the drains and close them out at this point.
		if val == 1 {
			if drain, ok := drains[Url]; ok {
				go drain.Close()
				delete(drains, Url)
				var index_to_remove = -1
				for i, drain_key := range drain_keys {
					if drain_key == Url {
						index_to_remove = i
					}
				}
				if index_to_remove != -1 {
					drain_keys = append(drain_keys[:index_to_remove], drain_keys[index_to_remove+1:]...)
				}
			} else {
				return fmt.Errorf("Unable to find active drains for record %s with url %s", Id, Url)
			}
		}
	} else {
		return fmt.Errorf("Unable to find any record %s with url %s", Id, Url)
	}
	return nil
}

func CloseAll() {
	for _, drain := range drain_keys {
		fmt.Printf("[drains]  Closing drain to %s...", drain)
		drains[drain].Close()
		fmt.Printf("\n")
	}
}

func Init() {
	drains_mutex = &sync.Mutex{}
	bad_hosts_mutex = &sync.Mutex{}
	drains = make(map[string]Drain)
	drain_keys = make([]string, 0)
	drains_count = make(map[string]int)
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
