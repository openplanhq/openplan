package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

const (
	// bridgeName is the executor's side of every sandbox's link.
	bridgeName = "opsb0"
	// nftTable holds every rule the sandboxes' network needs, so the
	// executor replaces it whole on each start.
	nftTable = "openplan_sandbox"
	// dnsRelayPort is where the relay listens on the gateway. Sandboxes ask
	// port 53, which nftables redirects here: the executor keeps no
	// capability to listen below 1024.
	dnsRelayPort = 1053
	// executorResolvConf is the executor's own resolver configuration.
	executorResolvConf = "/etc/resolv.conf"
	// sandboxLink is the sandbox's end of its veth pair, as gVisor sees it.
	sandboxLink = "eth0"
	// nftTimeout bounds installing the rules.
	nftTimeout = 30 * time.Second
)

// Network wires each sandbox to the executor's network (#245). runsc's
// sandbox networking moves a namespace's addresses into gVisor, so a sandbox
// can't share the executor's namespace without taking its address. Each
// gets a namespace of its own instead, whose eth0 is one end of a veth pair;
// the other end is an isolated port on a bridge, so sandboxes reach the
// executor's network, through NAT, but not each other.
type Network struct {
	subnet  netip.Prefix
	gateway netip.Addr
	mtu     int
	bridge  int
	// namespaces is where each sandbox's namespace is pinned, and
	// resolvConf the resolver configuration every sandbox mounts.
	namespaces string
	resolvConf string
	relay      *dnsRelay

	mu   sync.Mutex
	pool *addressPool
}

// SetupNetwork readies the executor to give sandboxes a network: IP
// forwarding, the bridge, the nftables rules and the DNS relay. State is
// runsc's state directory, ephemeral, like the namespaces pinned there. The
// executor calls it once at startup and refuses to start if it fails.
func SetupNetwork(state string, subnet netip.Prefix) (*Network, error) {
	subnet = subnet.Masked()
	if !subnet.Addr().Is4() || subnet.Bits() > 29 {
		return nil, fmt.Errorf("sandbox subnet %s: want IPv4, /29 or larger", subnet)
	}
	if err := enableForwarding(); err != nil {
		return nil, err
	}
	n := &Network{
		subnet:     subnet,
		gateway:    subnet.Addr().Next(),
		mtu:        defaultRouteMTU(),
		namespaces: filepath.Join(state, "netns"),
		resolvConf: filepath.Join(state, "resolv.conf"),
		pool:       newAddressPool(subnet),
	}
	if err := os.MkdirAll(n.namespaces, 0o700); err != nil {
		return nil, err
	}
	if err := n.makeBridge(); err != nil {
		return nil, fmt.Errorf("make the sandboxes' bridge: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), nftTimeout)
	defer cancel()
	nft := exec.CommandContext(ctx, "nft", "-f", "-")
	nft.Stdin = strings.NewReader(natRules(bridgeName, subnet, n.gateway, dnsRelayPort))
	if out, err := nft.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("install the sandboxes' nftables rules: %w: %s", err, bytes.TrimSpace(out))
	}
	executor, err := os.ReadFile(executorResolvConf)
	if err != nil {
		return nil, fmt.Errorf("read the executor's resolvers: %w", err)
	}
	if err := os.WriteFile(n.resolvConf, sandboxResolvConf(executor, n.gateway), 0o644); err != nil { //nolint:gosec // every sandbox reads it
		return nil, err
	}
	n.relay, err = startDNSRelay(netip.AddrPortFrom(n.gateway, dnsRelayPort).String(), resolvers(executor))
	if err != nil {
		return nil, fmt.Errorf("start the sandboxes' DNS relay: %w", err)
	}
	return n, nil
}

// enableForwarding turns on IP forwarding in the executor's namespace, which
// sandbox traffic needs to leave it, or reports how to.
func enableForwarding() error {
	const path = "/proc/sys/net/ipv4/ip_forward"
	if current, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(current)) == "1" {
		return nil
	}
	if err := os.WriteFile(path, []byte("1"), 0o644); err != nil { //nolint:gosec // a sysctl
		return fmt.Errorf("IP forwarding is off and can't be turned on (%w): set net.ipv4.ip_forward=1 for the executor (Compose: sysctls)", err)
	}
	return nil
}

// defaultRouteMTU is the MTU of the executor's way out, so a sandbox's
// packets fit it without fragmenting.
func defaultRouteMTU() int {
	routes, err := netlink.RouteGet(net.IPv4(1, 1, 1, 1))
	if err == nil && len(routes) > 0 {
		if link, err := netlink.LinkByIndex(routes[0].LinkIndex); err == nil && link.Attrs().MTU > 0 {
			return link.Attrs().MTU
		}
	}
	return 1500
}

func (n *Network) makeBridge() error {
	// A bridge left by an earlier executor in this namespace goes, with
	// anything still attached to it.
	if old, err := netlink.LinkByName(bridgeName); err == nil {
		if err := netlink.LinkDel(old); err != nil {
			return err
		}
	}
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName, MTU: n.mtu}}
	if err := netlink.LinkAdd(bridge); err != nil {
		return err
	}
	address := &netlink.Addr{IPNet: &net.IPNet{IP: n.gateway.AsSlice(), Mask: net.CIDRMask(n.subnet.Bits(), 32)}}
	if err := netlink.AddrAdd(bridge, address); err != nil {
		return err
	}
	n.bridge = bridge.Attrs().Index
	return netlink.LinkSetUp(bridge)
}

// natRules is the whole nftables table: sandboxes' traffic leaves as the
// executor's, their DNS goes to the relay, and nothing passes from one
// sandbox to another. Declaring the table before deleting it makes the
// delete safe on a first start.
func natRules(bridge string, subnet netip.Prefix, gateway netip.Addr, relayPort uint16) string {
	return fmt.Sprintf(`table ip %[1]s
delete table ip %[1]s
table ip %[1]s {
	chain prerouting {
		type nat hook prerouting priority dstnat; policy accept;
		iifname %[2]q ip daddr %[4]s meta l4proto { tcp, udp } th dport 53 redirect to :%[5]d
	}
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
		ip saddr %[3]s oifname != %[2]q masquerade
	}
	chain forward {
		type filter hook forward priority filter; policy accept;
		iifname %[2]q oifname %[2]q drop
	}
}
`, nftTable, bridge, subnet, gateway, relayPort)
}

// sandboxResolvConf is the executor's resolver configuration with the
// gateway, where the relay answers, as the only nameserver. Search domains
// and options carry over, so names resolve in a sandbox as they do here.
func sandboxResolvConf(executor []byte, gateway netip.Addr) []byte {
	out := []byte("nameserver " + gateway.String() + "\n")
	for line := range strings.SplitSeq(string(executor), "\n") {
		if field, _, _ := strings.Cut(strings.TrimSpace(line), " "); field == "search" || field == "options" {
			out = append(out, strings.TrimSpace(line)+"\n"...)
		}
	}
	return out
}

// resolvers are the executor's nameservers, as addresses to dial.
func resolvers(executor []byte) []string {
	var out []string
	scanner := bufio.NewScanner(bytes.NewReader(executor))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			out = append(out, net.JoinHostPort(fields[1], "53"))
		}
	}
	return out
}

// addressPool hands out the subnet's host addresses, past the gateway, to
// one sandbox each.
type addressPool struct {
	subnet netip.Prefix
	used   map[netip.Addr]bool
}

func newAddressPool(subnet netip.Prefix) *addressPool {
	return &addressPool{subnet: subnet, used: map[netip.Addr]bool{}}
}

// take returns the lowest free address.
func (p *addressPool) take() (netip.Addr, error) {
	// Past the network address and the gateway; the last is broadcast.
	for addr := p.subnet.Addr().Next().Next(); p.subnet.Contains(addr.Next()); addr = addr.Next() {
		if !p.used[addr] {
			p.used[addr] = true
			return addr, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("every address in the sandbox subnet %s is taken", p.subnet)
}

func (p *addressPool) give(addr netip.Addr) { delete(p.used, addr) }

// hostLinkName names a sandbox's end of its veth pair on the executor: "op"
// and the sandbox ID's last twelve hex digits, within Linux's 15 bytes.
func hostLinkName(id string) string {
	return "op" + id[max(0, len(id)-12):]
}

// attachment is one sandbox's network: its pinned namespace, its address and
// the executor's end of its veth pair.
type attachment struct {
	namespace string
	addr      netip.Addr
	hostLink  string
}

// attach makes a sandbox's namespace and wires it to the bridge, with its
// own address and a default route through the gateway, for runsc to copy
// into gVisor. A failure undoes whatever it made.
func (n *Network) attach(id string) (att *attachment, err error) {
	n.mu.Lock()
	addr, err := n.pool.take()
	n.mu.Unlock()
	if err != nil {
		return nil, err
	}
	att = &attachment{namespace: filepath.Join(n.namespaces, id), addr: addr, hostLink: hostLinkName(id)}
	defer func() {
		if err != nil {
			n.release(att)
			att = nil
		}
	}()
	if err := pinNewNamespace(att.namespace); err != nil {
		return nil, fmt.Errorf("make the sandbox's network namespace: %w", err)
	}
	ns, err := netns.GetFromPath(att.namespace)
	if err != nil {
		return nil, err
	}
	defer ns.Close()
	veth := &netlink.Veth{
		LinkAttrs:     netlink.LinkAttrs{Name: att.hostLink, MTU: n.mtu, MasterIndex: n.bridge},
		PeerName:      sandboxLink,
		PeerNamespace: netlink.NsFd(ns),
		PeerMTU:       uint32(n.mtu), //nolint:gosec // an MTU
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return nil, fmt.Errorf("make the sandbox's veth pair: %w", err)
	}
	if err := netlink.LinkSetIsolated(veth, true); err != nil {
		return nil, fmt.Errorf("isolate the sandbox's bridge port: %w", err)
	}
	if err := netlink.LinkSetUp(veth); err != nil {
		return nil, err
	}
	if err := n.configureInside(ns, addr); err != nil {
		return nil, fmt.Errorf("configure the sandbox's network: %w", err)
	}
	return att, nil
}

// configureInside gives the namespace's eth0 its address and default route,
// and brings up loopback.
func (n *Network) configureInside(ns netns.NsHandle, addr netip.Addr) error {
	handle, err := netlink.NewHandleAt(ns)
	if err != nil {
		return err
	}
	defer handle.Close()
	for _, name := range []string{"lo", sandboxLink} {
		link, err := handle.LinkByName(name)
		if err != nil {
			return err
		}
		if name == sandboxLink {
			address := &netlink.Addr{IPNet: &net.IPNet{IP: addr.AsSlice(), Mask: net.CIDRMask(n.subnet.Bits(), 32)}}
			if err := handle.AddrAdd(link, address); err != nil {
				return err
			}
		}
		if err := handle.LinkSetUp(link); err != nil {
			return err
		}
		if name == sandboxLink {
			if err := handle.RouteAdd(&netlink.Route{LinkIndex: link.Attrs().Index, Gw: n.gateway.AsSlice()}); err != nil {
				return err
			}
		}
	}
	return nil
}

// release undoes attach. The namespace goes once its pin does and the
// sandbox has left it, and the veth pair with it; the host end is deleted
// too, should anything still hold the namespace.
func (n *Network) release(att *attachment) {
	if link, err := netlink.LinkByName(att.hostLink); err == nil {
		_ = netlink.LinkDel(link)
	}
	_ = unix.Unmount(att.namespace, unix.MNT_DETACH)
	_ = os.Remove(att.namespace)
	n.mu.Lock()
	n.pool.give(att.addr)
	n.mu.Unlock()
}

// pinNewNamespace makes a network namespace and bind-mounts it at path,
// where runsc joins it by name.
func pinNewNamespace(path string) error {
	file, err := os.Create(path) //nolint:gosec // under runsc's state directory
	if err != nil {
		return err
	}
	_ = file.Close()
	done := make(chan error, 1)
	go func() {
		// Unsharing moves only this thread into the new namespace. It never
		// unlocks, so Go retires the thread with the goroutine rather than
		// run anything else in that namespace.
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			done <- err
			return
		}
		self := "/proc/self/task/" + strconv.Itoa(unix.Gettid()) + "/ns/net"
		done <- unix.Mount(self, path, "", unix.MS_BIND, "")
	}()
	if err := <-done; err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
