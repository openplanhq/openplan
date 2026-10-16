package sandbox

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	// dnsTimeout bounds one resolver's answer, and a whole TCP exchange.
	dnsTimeout = 5 * time.Second
	// dnsInFlight bounds the queries the relay works on at once, so a
	// sandbox flooding it can't make the executor start goroutines without
	// end.
	dnsInFlight = 256
)

// dnsRelay passes sandboxes' DNS queries, unread, to the executor's own
// resolvers and their answers back. A sandbox can't use those resolvers
// directly: Docker's embedded DNS answers only on the executor's loopback,
// which a sandbox's network namespace doesn't share.
type dnsRelay struct {
	resolvers []string
	udp       *net.UDPConn
	tcp       *net.TCPListener
	slots     chan struct{}
	done      sync.WaitGroup
}

// startDNSRelay listens on listen, over UDP and TCP, and serves until close.
func startDNSRelay(listen string, resolvers []string) (*dnsRelay, error) {
	if len(resolvers) == 0 {
		return nil, errors.New("no resolver to relay DNS to")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", listen)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	// The same port over TCP, which a resolver falls back to for answers
	// too long for UDP.
	tcp, err := net.ListenTCP("tcp", &net.TCPAddr{IP: udp.LocalAddr().(*net.UDPAddr).IP, Port: udp.LocalAddr().(*net.UDPAddr).Port}) //nolint:forcetypeassert // a UDPConn's address is a UDPAddr
	if err != nil {
		_ = udp.Close()
		return nil, err
	}
	r := &dnsRelay{resolvers: resolvers, udp: udp, tcp: tcp, slots: make(chan struct{}, dnsInFlight)}
	r.done.Add(2)
	go r.serveUDP()
	go r.serveTCP()
	return r, nil
}

func (r *dnsRelay) addr() string { return r.udp.LocalAddr().String() }

func (r *dnsRelay) close() {
	_ = r.udp.Close()
	_ = r.tcp.Close()
	r.done.Wait()
}

func (r *dnsRelay) serveUDP() {
	defer r.done.Done()
	for {
		buf := make([]byte, 65535)
		n, from, err := r.udp.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		select {
		case r.slots <- struct{}{}:
		default:
			// Full: drop it, as a busy resolver would; the client retries.
			continue
		}
		go func() {
			defer func() { <-r.slots }()
			if answer := r.askUDP(buf[:n]); answer != nil {
				_, _ = r.udp.WriteToUDP(answer, from)
			}
		}()
	}
}

// askUDP asks each resolver in turn and returns the first answer.
func (r *dnsRelay) askUDP(query []byte) []byte {
	for _, resolver := range r.resolvers {
		conn, err := dial("udp", resolver)
		if err != nil {
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(dnsTimeout))
		answer := make([]byte, 65535)
		var n int
		if _, err = conn.Write(query); err == nil {
			n, err = conn.Read(answer)
		}
		_ = conn.Close()
		if err == nil {
			return answer[:n]
		}
	}
	return nil
}

// dial connects to a resolver within dnsTimeout.
func dial(network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func (r *dnsRelay) serveTCP() {
	defer r.done.Done()
	for {
		conn, err := r.tcp.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		select {
		case r.slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		go func() {
			defer func() { <-r.slots }()
			r.relayTCP(conn)
		}()
	}
}

// relayTCP joins one client connection to the first resolver that accepts
// one, both ways, for at most dnsTimeout.
func (r *dnsRelay) relayTCP(client net.Conn) {
	defer client.Close()
	var resolver net.Conn
	for _, address := range r.resolvers {
		if conn, err := dial("tcp", address); err == nil {
			resolver = conn
			break
		}
	}
	if resolver == nil {
		return
	}
	defer resolver.Close()
	deadline := time.Now().Add(dnsTimeout)
	_ = client.SetDeadline(deadline)
	_ = resolver.SetDeadline(deadline)
	go func() {
		_, _ = io.Copy(resolver, client)
		// The query is in; let the resolver see the end of it.
		if tcp, ok := resolver.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	_, _ = io.Copy(client, resolver)
}
