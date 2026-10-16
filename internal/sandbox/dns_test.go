package sandbox

import (
	"encoding/binary"
	"io"
	"net"
	"slices"
	"testing"
	"time"
)

// fakeResolver answers every query, over UDP and TCP, with the query
// reversed, so a test can tell an answer from an echo.
func fakeResolver(t *testing.T) string {
	t.Helper()
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(reversed(buf[:n]), from)
		}
	}()
	// The same port over TCP, as a resolver listens.
	tcp, err := net.Listen("tcp", udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tcp.Close() })
	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			var size [2]byte
			if _, err := io.ReadFull(conn, size[:]); err == nil {
				query := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, query); err == nil {
					_, _ = conn.Write(append(size[:], reversed(query)...))
				}
			}
			_ = conn.Close()
		}
	}()
	return udp.LocalAddr().String()
}

func reversed(b []byte) []byte {
	out := slices.Clone(b)
	slices.Reverse(out)
	return out
}

func TestDNSRelayPassesUDPQueriesOn(t *testing.T) {
	t.Parallel()

	relay, err := startDNSRelay("127.0.0.1:0", []string{fakeResolver(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.close)

	conn, err := net.Dial("udp", relay.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("query")); err != nil {
		t.Fatal(err)
	}
	answer := make([]byte, 512)
	n, err := conn.Read(answer)
	if err != nil || string(answer[:n]) != "yreuq" {
		t.Fatalf("answer = %q, %v; want the resolver's", answer[:n], err)
	}
}

func TestDNSRelayPassesTCPQueriesOn(t *testing.T) {
	t.Parallel()

	relay, err := startDNSRelay("127.0.0.1:0", []string{fakeResolver(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.close)

	conn, err := net.Dial("tcp", relay.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte{0, 5, 'q', 'u', 'e', 'r', 'y'}); err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(conn)
	if err != nil || string(answer) != "\x00\x05yreuq" {
		t.Fatalf("answer = %q, %v; want the resolver's", answer, err)
	}
}

// A resolver that doesn't answer is skipped for the next.
func TestDNSRelayTriesTheNextResolver(t *testing.T) {
	t.Parallel()

	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	relay, err := startDNSRelay("127.0.0.1:0", []string{silent.LocalAddr().String(), fakeResolver(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.close)

	conn, err := net.Dial("udp", relay.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte("query")); err != nil {
		t.Fatal(err)
	}
	answer := make([]byte, 512)
	n, err := conn.Read(answer)
	if err != nil || string(answer[:n]) != "yreuq" {
		t.Fatalf("answer = %q, %v; want the second resolver's", answer[:n], err)
	}
}
