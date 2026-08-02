package bt

import (
	"net"
	"testing"

	"github.com/anacrolix/torrent"
)

func TestBlockerNetworkAndPort(t *testing.T) {
	blocker, err := NewBlocker(BlockConfig{
		Ports:    []int{6881},
		Networks: []string{"203.0.113.0/24", "198.51.100.10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := blocker.Lookup(net.ParseIP("203.0.113.50")); !ok {
		t.Fatal("expected CIDR match")
	}
	if _, ok := blocker.Lookup(net.ParseIP("198.51.100.10")); !ok {
		t.Fatal("expected exact IP match")
	}
	if _, ok := blocker.Lookup(net.ParseIP("198.51.100.11")); ok {
		t.Fatal("unexpected IP match")
	}

	addr := &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 6881}
	if reason, blocked := blocker.shouldBlock("", torrent.PeerID{}, addr, 0); !blocked || reason == "" {
		t.Fatalf("expected port block, got %q %v", reason, blocked)
	}
}

func TestBlockerClientMatch(t *testing.T) {
	blocker, err := NewBlocker(BlockConfig{Clients: []string{"xunlei", "qB"}})
	if err != nil {
		t.Fatal(err)
	}
	var id torrent.PeerID
	copy(id[:], []byte("-XL0012-abcdefghijk"))
	if reason, blocked := blocker.shouldBlock("", id, nil, 0); !blocked {
		t.Fatalf("expected Xunlei block, reason=%q", reason)
	}
	copy(id[:], []byte("-qB4500-abcdefghijk"))
	if _, blocked := blocker.shouldBlock("", id, nil, 0); !blocked {
		t.Fatal("expected qBittorrent block via code")
	}
	copy(id[:], []byte("-TR4050-abcdefghijk"))
	if _, blocked := blocker.shouldBlock("Transmission 4.0.5", id, nil, 0); blocked {
		t.Fatal("Transmission should not be blocked")
	}
}

func TestBlockerRejectsBadPort(t *testing.T) {
	if _, err := NewBlocker(BlockConfig{Ports: []int{70000}}); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestBlockerPeerIDMatch(t *testing.T) {
	var id torrent.PeerID
	copy(id[:], []byte("-qB4500-abcdefghijk"))
	formatted := formatPeerID(id)
	blocker, err := NewBlocker(BlockConfig{PeerIDs: []string{formatted}})
	if err != nil {
		t.Fatal(err)
	}
	if _, blocked := blocker.shouldBlock("", id, nil, 0); !blocked {
		t.Fatal("expected peer id block")
	}
}
