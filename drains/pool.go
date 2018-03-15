package drains

import (
	"../syslog"
	"time"
	"strings"
	"fmt"
	"hash/crc32"
	"sync"
	"sync/atomic"
)

type Pool struct {
	MaxConnections uint32
	initialConnections int
	bufferSize int
	destinationUrl string
	Packets chan syslog.Packet
	stopChan chan struct{}
	conns []*syslog.Logger
	Attempting uint32
	Sent uint32
	Mutex *sync.Mutex
	Pressure float64
}

func (p *Pool) connect(increasePool bool, pressure float64) error {
	if atomic.LoadUint32(&p.Attempting) == 1 {
		return nil
	}
	atomic.StoreUint32(&p.Attempting, 1)
	var network = "tls"
	var Url = p.destinationUrl
	var host = Url
	if strings.HasPrefix(Url, "syslog+tcp://") || strings.HasPrefix(Url, "syslog://") || strings.HasPrefix(Url, "tcp://") {
		network = "tcp"
	} else if strings.HasPrefix(Url, "syslog+udp://") || strings.HasPrefix(Url, "udp://") {
		network = "udp"
	} else if strings.HasPrefix(Url, "syslog+tls://") || strings.HasPrefix(Url, "ssh://") {
		network = "tls"
	} else {
		atomic.StoreUint32(&p.Attempting, 0)
		return fmt.Errorf("[pool] Warning unknown url schema provided: %s", Url)
	}

	if strings.Contains(Url, "://") {
		host = strings.Split(Url, "://")[1]
	}

	fmt.Printf("[pool] Opening connection to %s\n", host)
	dest, err := syslog.Dial("logshuttle.akkeris.local", network, host, nil, time.Second*4, time.Second*4, MaxLogSize)
	if err != nil {
		atomic.StoreUint32(&p.Attempting, 0)
		return fmt.Errorf("[pool] Unable to establish connection to %s:", err)
	}
	if dest == nil {
		atomic.StoreUint32(&p.Attempting, 0)
		return fmt.Errorf("[pool] Unable to establish connection, no known error %s", host)
	}
	p.conns = append(p.conns, dest)

	if increasePool {
		fmt.Printf("[pool] Increasing pool size for %s to %d because back pressure was %f%%\n", p.destinationUrl, p.OpenConnections(), pressure * 100)
	}
	atomic.StoreUint32(&p.Attempting, 0)
	return nil
}

func (p *Pool) OpenConnections() uint32 {
	return uint32(len(p.conns))
}

func (p *Pool) PrintMetrics() {
	p.Mutex.Lock()
	fmt.Printf("[metrics] syslog=%s max#connections=%d count#connections=%d measure#pressure=%f%% count#sent=%d\n", p.destinationUrl, p.MaxConnections, p.OpenConnections(), p.Pressure * 100, p.Sent)
	if p.Pressure > 0.98 && p.OpenConnections() == p.MaxConnections {
		fmt.Printf("[alert] We've reached our maximum allocated connection count %d and our back pressure is still high %f.\n[alert] If this isn't during startup this could mean a loss of log data.\n", p.OpenConnections(), p.Pressure * 100 )
	}
	p.Sent = 0
	p.Mutex.Unlock()
}

func (p *Pool) Init(DestinationUrl string) error {
	p.MaxConnections = 20
	p.initialConnections = 1
	p.bufferSize = 512
	p.destinationUrl = DestinationUrl
	p.Packets = make(chan syslog.Packet, p.bufferSize)
	p.stopChan = make(chan struct{})
	p.conns = make([]*syslog.Logger, 0)
	atomic.StoreUint32(&p.Attempting, 0)
	p.Sent = 0
	p.Mutex = &sync.Mutex{}
	p.Pressure = 0

	fmt.Printf("[pool] Creating pool to %s\n", p.destinationUrl)
	for i := 0; i < p.initialConnections; i++ {
		if i > 0 {
			// throttle connections so we don't upset our upstream neighbors.
			time.Sleep(time.Millisecond * 500)
		}
		if err := p.connect(false, 0); err != nil {
			fmt.Printf("[pool] Connection was closed to %s due to %s\n", p.destinationUrl, err)
		}
	}
	if(p.OpenConnections() == 0) {
		return fmt.Errorf("[pool] Unable to establish connection to %s", p.destinationUrl)
	}
	go p.writeLoop()

	fmt.Printf("[pool] Pool successfully created for %s\n", p.destinationUrl)
	return nil
}

func (p *Pool) Close() {
	for i := 0; i < int(p.OpenConnections()); i++ {
		p.conns[i].Close()
	}
	p.stopChan <- struct{}{}
}

func (p *Pool) writeLoop() {
	for {
		select {
			case packet := <- p.Packets:
				p.Mutex.Lock()
				p.Sent++
				// Ensure the same host goes down the same connection so we keep logs in 
				// order, this could hypothetically cause "hot" connections. Use a CRC to
				// calculate a int32 then mod (bound it) to the amount of open connections
				// so its deterministic in the connection it picks.
				ndx := uint32(crc32.ChecksumIEEE([]byte(packet.Tag)) % p.OpenConnections())
				p.conns[ndx].Packets <- packet
				p.Pressure = (p.Pressure + (float64(len(p.Packets)) / float64(cap(p.Packets)))) / float64(2)
				if(p.Pressure > 0.1 && p.OpenConnections() < p.MaxConnections) {
					go p.connect(true, p.Pressure)
				}
				p.Mutex.Unlock()
			case <- p.stopChan:
				return
		}
	}
}