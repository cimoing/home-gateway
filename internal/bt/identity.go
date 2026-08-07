package bt

import (
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/version"
)

const (
	// ClientName is the BitTorrent client type advertised to peers.
	ClientName = "anacrolix"
)

func applyClientIdentity(config *torrent.ClientConfig) {
	// Match anacrolix/torrent defaults: Peer ID -GT0003-, UA anacrolix-torrent/<ver>.
	config.Bep20 = version.DefaultBep20Prefix
	config.HTTPUserAgent = version.DefaultHttpUserAgent
	config.ExtendedHandshakeClientVersion = version.DefaultHttpUserAgent
	config.UpnpID = ClientName
}
