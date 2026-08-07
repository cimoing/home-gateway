package bt

import "testing"

func TestIdentifyPeerClientFromPeerID(t *testing.T) {
	var id [20]byte
	copy(id[:], []byte("-qB4500-abcdefghijk"))
	client, version := identifyPeerClient("", id)
	if client != "qBittorrent" {
		t.Fatalf("client = %q, want qBittorrent", client)
	}
	if version != "4.5.0" {
		t.Fatalf("version = %q, want 4.5.0", version)
	}
}

func TestIdentifyPeerClientPrefersExtendedHandshake(t *testing.T) {
	var id [20]byte
	copy(id[:], []byte("-TR4050-abcdefghijk"))
	client, version := identifyPeerClient("Transmission 4.0.6", id)
	if client != "Transmission" {
		t.Fatalf("client = %q, want Transmission", client)
	}
	if version != "4.0.6" {
		t.Fatalf("version = %q, want 4.0.6", version)
	}
}

func TestIdentifyPeerClientSlashVersion(t *testing.T) {
	client, version := identifyPeerClient("qBittorrent/5.0.1", [20]byte{})
	if client != "qBittorrent" || version != "5.0.1" {
		t.Fatalf("got %q %q", client, version)
	}
}

func TestIdentifyPeerClientUnknownID(t *testing.T) {
	var id [20]byte
	copy(id[:], []byte("xxxxxxxxxxxxxxxxxxxx"))
	client, version := identifyPeerClient("", id)
	if client != "" || version != "" {
		t.Fatalf("got %q %q, want empty", client, version)
	}
}

func TestIdentifyPeerClientAnacrolix(t *testing.T) {
	var id [20]byte
	copy(id[:], []byte("-GT0003-abcdefghijk"))
	client, version := identifyPeerClient("", id)
	if client != "anacrolix" {
		t.Fatalf("client = %q, want anacrolix", client)
	}
	if version != "0.0.0.3" {
		t.Fatalf("version = %q, want 0.0.0.3", version)
	}
}
