package people

import (
	"encoding/json"
	"net"
	"sync"
	"time"
)

const udpPort = 36421

type LAN struct {
	mu      sync.Mutex
	conn    *net.UDPConn
	stop    chan struct{}
	running bool
}

func NewLAN() *LAN { return &LAN{} }

func (l *LAN) Start(beacon func() Beacon, onPeer func(Beacon, string)) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil
	}
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: udpPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	l.conn = conn
	l.stop = make(chan struct{})
	l.running = true
	stop := l.stop
	l.mu.Unlock()
	go l.readLoop(conn, stop, onPeer)
	go l.broadcastLoop(conn, stop, beacon)
	return nil
}

func (l *LAN) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	close(l.stop)
	_ = l.conn.Close()
	l.running = false
	l.conn = nil
	l.stop = nil
}

func (l *LAN) readLoop(conn *net.UDPConn, stop <-chan struct{}, onPeer func(Beacon, string)) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var b Beacon
		if json.Unmarshal(buf[:n], &b) != nil {
			continue
		}
		if onPeer != nil {
			host := ""
			if from != nil {
				host = from.IP.String()
			}
			onPeer(b, host)
		}
	}
}

func (l *LAN) broadcastLoop(conn *net.UDPConn, stop <-chan struct{}, beacon func() Beacon) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	send := func() {
		if beacon == nil {
			return
		}
		b := beacon()
		if b.SubjectID == "" || b.Status == "invisible" {
			return
		}
		raw, err := json.Marshal(b)
		if err != nil {
			return
		}
		dst := &net.UDPAddr{IP: net.IPv4bcast, Port: udpPort}
		_, _ = conn.WriteToUDP(raw, dst)
	}
	send()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			send()
		}
	}
}
