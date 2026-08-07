package bt

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	pp "github.com/anacrolix/torrent/peer_protocol"
)

// BlockConfig is the YAML-backed peer blocklist.
type BlockConfig struct {
	Clients  []string `yaml:"clients" json:"clients"`
	PeerIDs  []string `yaml:"peer_ids" json:"peerIds"`
	Ports    []int    `yaml:"ports" json:"ports"`
	Networks []string `yaml:"networks" json:"networks"`
}

// Blocker enforces IP/CIDR, port, client, and peer ID block rules.
// It also implements iplist.Ranger for early IP rejection by the torrent client.
type Blocker struct {
	mu       sync.RWMutex
	clients  []string
	peerIDs  map[string]struct{}
	ports    map[int]struct{}
	networks []*net.IPNet
}

// NewBlocker builds a blocker from config. Invalid entries return an error.
func NewBlocker(config BlockConfig) (*Blocker, error) {
	blocker := &Blocker{}
	if err := blocker.Replace(config); err != nil {
		return nil, err
	}
	return blocker, nil
}

// Replace atomically swaps the active rules.
func (b *Blocker) Replace(config BlockConfig) error {
	clients := make([]string, 0, len(config.Clients))
	for _, client := range config.Clients {
		client = strings.ToLower(strings.TrimSpace(client))
		if client == "" {
			continue
		}
		clients = append(clients, client)
	}

	peerIDs := make(map[string]struct{}, len(config.PeerIDs))
	for _, peerID := range config.PeerIDs {
		peerID = strings.TrimSpace(peerID)
		if peerID == "" {
			continue
		}
		peerIDs[peerID] = struct{}{}
	}

	ports := make(map[int]struct{}, len(config.Ports))
	for _, port := range config.Ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("block port %d is out of range", port)
		}
		ports[port] = struct{}{}
	}

	networks := make([]*net.IPNet, 0, len(config.Networks))
	for _, entry := range config.Networks {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		network, err := parseIPOrCIDR(entry)
		if err != nil {
			return fmt.Errorf("block network %q: %w", entry, err)
		}
		networks = append(networks, network)
	}

	b.mu.Lock()
	b.clients = clients
	b.peerIDs = peerIDs
	b.ports = ports
	b.networks = networks
	b.mu.Unlock()
	return nil
}

// Lookup implements iplist.Ranger for IPBlocklist integration.
func (b *Blocker) Lookup(ip net.IP) (iplist.Range, bool) {
	if b == nil || ip == nil {
		return iplist.Range{}, false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, network := range b.networks {
		if network.Contains(ip) {
			return iplist.Range{
				First:       ip,
				Last:        ip,
				Description: "blocked by config",
			}, true
		}
	}
	return iplist.Range{}, false
}

// NumRanges implements iplist.Ranger.
func (b *Blocker) NumRanges() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.networks)
}

func (b *Blocker) shouldBlock(extName string, peerID torrent.PeerID, remote torrent.PeerRemoteAddr, listenPort int) (reason string, blocked bool) {
	if b == nil {
		return "", false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	ip, port, ok := splitRemoteAddr(remote)
	if ok {
		for _, network := range b.networks {
			if network.Contains(ip) {
				return "network " + network.String(), true
			}
		}
		if _, blocked := b.ports[port]; blocked {
			return fmt.Sprintf("port %d", port), true
		}
	}
	if listenPort > 0 {
		if _, blocked := b.ports[listenPort]; blocked {
			return fmt.Sprintf("listen port %d", listenPort), true
		}
	}
	formattedID := formatPeerID(peerID)
	if _, blocked := b.peerIDs[formattedID]; blocked {
		return "peer id " + formattedID, true
	}
	if matched, pattern := b.matchClientLocked(extName, peerID); matched {
		return "client " + pattern, true
	}
	return "", false
}

func (b *Blocker) matchClientLocked(extName string, peerID torrent.PeerID) (bool, string) {
	if len(b.clients) == 0 {
		return false, ""
	}
	client, version := identifyPeerClient(extName, peerID)
	code := ""
	if peerID[0] == '-' && peerID[7] == '-' {
		code = string(peerID[1:3])
	}
	haystack := strings.ToLower(strings.Join([]string{
		client,
		version,
		extName,
		formatPeerID(peerID),
		code,
	}, "\n"))
	for _, pattern := range b.clients {
		if strings.Contains(haystack, pattern) {
			return true, pattern
		}
	}
	return false, ""
}

func (b *Blocker) install(config *torrent.ClientConfig) {
	if b == nil || config == nil {
		return
	}
	config.IPBlocklist = b
	config.Callbacks.PeerConnAdded = append(config.Callbacks.PeerConnAdded, b.onPeerConnAdded)
	config.Callbacks.ReadExtendedHandshake = b.chainReadExtendedHandshake(config.Callbacks.ReadExtendedHandshake)
}

func (b *Blocker) onPeerConnAdded(conn *torrent.PeerConn) {
	reason, blocked := b.shouldBlock("", conn.PeerID, conn.RemoteAddr, conn.PeerListenPort)
	if !blocked {
		return
	}
	log.Printf("BT blocked peer %s (%s)", conn.RemoteAddr, reason)
	dropPeerConn(conn)
}

func (b *Blocker) chainReadExtendedHandshake(
	previous func(*torrent.PeerConn, *pp.ExtendedHandshakeMessage),
) func(*torrent.PeerConn, *pp.ExtendedHandshakeMessage) {
	return func(conn *torrent.PeerConn, msg *pp.ExtendedHandshakeMessage) {
		if previous != nil {
			previous(conn, msg)
		}
		extName := ""
		if msg != nil {
			extName = msg.V
		}
		listenPort := conn.PeerListenPort
		if msg != nil && msg.Port != 0 {
			listenPort = msg.Port
		}
		reason, blocked := b.shouldBlock(extName, conn.PeerID, conn.RemoteAddr, listenPort)
		if !blocked {
			return
		}
		log.Printf("BT blocked peer %s (%s)", conn.RemoteAddr, reason)
		dropPeerConn(conn)
	}
}

func dropPeerConn(conn *torrent.PeerConn) {
	if conn == nil {
		return
	}
	go func() { _ = conn.Close() }()
}

func parseIPOrCIDR(value string) (*net.IPNet, error) {
	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		return network, nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP or CIDR")
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return &net.IPNet{IP: ip.To16(), Mask: net.CIDRMask(128, 128)}, nil
}

func splitRemoteAddr(addr torrent.PeerRemoteAddr) (net.IP, int, bool) {
	if addr == nil {
		return nil, 0, false
	}
	switch typed := addr.(type) {
	case *net.TCPAddr:
		return typed.IP, typed.Port, typed.IP != nil
	case *net.UDPAddr:
		return typed.IP, typed.Port, typed.IP != nil
	case net.Addr:
		host, portText, err := net.SplitHostPort(typed.String())
		if err != nil {
			return nil, 0, false
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, 0, false
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return nil, 0, false
		}
		return ip, port, true
	default:
		host, portText, err := net.SplitHostPort(addr.String())
		if err != nil {
			return nil, 0, false
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, 0, false
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return nil, 0, false
		}
		return ip, port, true
	}
}
