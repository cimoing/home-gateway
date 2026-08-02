package bt

const (
	// ClientName is the BitTorrent client type advertised to peers.
	ClientName = "Biubiubiu"
	// ClientVersion is the advertised client version.
	ClientVersion = "1.0.0"
	// ClientBep20 is the Azureus-style peer ID prefix (BEP 20).
	ClientBep20 = "-BU1000-"
)

func clientExtendedHandshakeVersion() string {
	return ClientName + "/" + ClientVersion
}

func clientHTTPUserAgent() string {
	return ClientName + "/" + ClientVersion
}

func clientUpnpID() string {
	return ClientName + " " + ClientVersion
}
