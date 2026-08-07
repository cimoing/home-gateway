package bt

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

// BlockConfig is the YAML-backed peer blocklist.
type BlockConfig struct {
	Clients  []string `yaml:"clients" json:"clients"`
	PeerIDs  []string `yaml:"peer_ids" json:"peerIds"`
	Ports    []int    `yaml:"ports" json:"ports"`
	Networks []string `yaml:"networks" json:"networks"`
}

// Blocker stores peer block rules for the RPC backend.
// Transmission cannot enforce client/peer-id/port handshake hooks; IP/CIDR
// entries are retained for UI/config and optional daemon-side blocklists.
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
