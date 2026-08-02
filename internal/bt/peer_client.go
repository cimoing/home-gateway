package bt

import (
	"strings"
	"unicode"
)

// azureusClients maps BEP 20 Azureus-style two-character codes to client names.
var azureusClients = map[string]string{
	"AG": "Ares",
	"A~": "Ares",
	"AR": "Arctic",
	"AT": "Artemis",
	"AV": "Avicora",
	"AX": "BitPump",
	"AZ": "Vuze",
	"BB": "BitBuddy",
	"BC": "BitComet",
	"BE": "Baretorrent",
	"BF": "Bitflu",
	"BG": "BTG",
	"BI": "BiglyBT",
	"BL": "BitLord",
	"BP": "BitTorrent Pro",
	"BR": "BitRocket",
	"BS": "BTSlave",
	"BT": "BitTorrent",
	"BU": "Biubiubiu",
	"BW": "BitWombat",
	"BX": "Bittorrent X",
	"CD": "Enhanced CTorrent",
	"CT": "CTorrent",
	"DE": "Deluge",
	"DP": "Propagate Data Client",
	"EB": "EBit",
	"ES": "electric sheep",
	"FC": "FileCroc",
	"FG": "FlashGet",
	"FT": "FoxTorrent",
	"GS": "GSTorrent",
	"HK": "Hekate",
	"HL": "Halite",
	"HM": "hMule",
	"HN": "Hydranode",
	"IL": "iLivid",
	"JS": "Justseed.it",
	"JT": "JavaTorrent",
	"KG": "KGet",
	"KT": "KTorrent",
	"LC": "LeechCraft",
	"LH": "LH-ABC",
	"LP": "Lphant",
	"LT": "libtorrent",
	"lt": "libTorrent",
	"LW": "LimeWire",
	"MK": "Meerkat",
	"MO": "MonoTorrent",
	"MP": "MooPolice",
	"MR": "Miro",
	"MT": "MoonlightTorrent",
	"NB": "NetTransport",
	"NX": "Net Transport",
	"OS": "OneSwarm",
	"OT": "OmegaTorrent",
	"PB": "Protocol::BitTorrent",
	"PD": "Pando",
	"PI": "PicoTorrent",
	"PT": "PHPTracker",
	"qB": "qBittorrent",
	"QD": "QQDownload",
	"QT": "Qt 4 Torrent",
	"RT": "Retriever",
	"RZ": "RezTorrent",
	"S~": "Shareaza",
	"SB": "SwiftBit",
	"SD": "Thunder",
	"SM": "SoMud",
	"SP": "BitSpirit",
	"SS": "SwarmScope",
	"ST": "SymTorrent",
	"st": "sharktorrent",
	"SZ": "Shareaza",
	"TB": "Torch",
	"TE": "tfleet",
	"TL": "Tribler",
	"TN": "Torrent.NET",
	"TR": "Transmission",
	"TS": "TorrentStorm",
	"TT": "TuoTu",
	"UL": "uLeecher!",
	"UM": "µTorrent Mac",
	"UT": "µTorrent",
	"VG": "Vagaa",
	"WD": "WebTorrent Desktop",
	"WT": "BitLet",
	"WW": "WebTorrent",
	"WY": "FireTorrent",
	"XF": "Xfplay",
	"XL": "Xunlei",
	"XS": "XSwifter",
	"XT": "XanTorrent",
	"XX": "Xtorrent",
	"ZT": "ZipTorrent",
}

// identifyPeerClient returns client type and version from an extended-handshake
// client string and/or a 20-byte peer ID (Azureus-style preferred).
func identifyPeerClient(extName string, peerID [20]byte) (client, version string) {
	extClient, extVersion := splitClientVersion(strings.TrimSpace(extName))
	idClient, idVersion := parseAzureusPeerID(peerID)

	switch {
	case extClient != "" && extVersion != "":
		return extClient, extVersion
	case extClient != "" && idVersion != "":
		return extClient, idVersion
	case extClient != "":
		return extClient, ""
	case idClient != "":
		return idClient, idVersion
	default:
		return "", ""
	}
}

func splitClientVersion(value string) (client, version string) {
	if value == "" {
		return "", ""
	}
	if i := strings.LastIndex(value, "/"); i > 0 && i < len(value)-1 {
		return strings.TrimSpace(value[:i]), strings.TrimSpace(value[i+1:])
	}
	parts := strings.Fields(value)
	if len(parts) >= 2 && looksLikeVersion(parts[len(parts)-1]) {
		return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
	}
	return value, ""
}

func looksLikeVersion(value string) bool {
	if value == "" {
		return false
	}
	digit := false
	for _, r := range value {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case r == '.' || r == '-' || r == '_':
			// ok
		default:
			return false
		}
	}
	return digit
}

func parseAzureusPeerID(id [20]byte) (client, version string) {
	if id[0] != '-' || id[7] != '-' {
		return "", ""
	}
	code := string(id[1:3])
	client = azureusClients[code]
	if client == "" {
		client = code
	}
	return client, formatAzureusVersion(string(id[3:7]))
}

func formatAzureusVersion(raw string) string {
	if len(raw) != 4 {
		return strings.TrimSpace(raw)
	}
	for i := 0; i < 4; i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return raw
		}
	}
	parts := []string{string(raw[0]), string(raw[1]), string(raw[2]), string(raw[3])}
	// Keep major.minor.patch; only drop a trailing zero build component.
	if parts[3] == "0" {
		parts = parts[:3]
	}
	return strings.Join(parts, ".")
}
