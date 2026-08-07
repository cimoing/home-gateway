package bt

import (
	"testing"
)

func TestNewBlockerRejectsInvalidNetwork(t *testing.T) {
	_, err := NewBlocker(BlockConfig{Networks: []string{"not-an-ip"}})
	if err == nil {
		t.Fatal("expected invalid network error")
	}
}

func TestNewBlockerAcceptsIPAndCIDR(t *testing.T) {
	blocker, err := NewBlocker(BlockConfig{
		Clients:  []string{"Xunlei", ""},
		Ports:    []int{6881, 6881},
		Networks: []string{"203.0.113.0/24", "198.51.100.10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocker == nil {
		t.Fatal("expected blocker")
	}
}
