package release

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// ParseMagnet builds a release from a magnet link. It needs no network: the
// file list is known once the download engine fetches the torrent metadata.
func ParseMagnet(link string) (Release, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "magnet" {
		return Release{}, errors.New("not a magnet link")
	}
	q := u.Query()
	hash := ""
	for _, xt := range q["xt"] {
		if v, ok := strings.CutPrefix(strings.ToLower(xt), "urn:btih:"); ok {
			hash = v
			break
		}
	}
	switch len(hash) {
	case 40:
		if _, err := hex.DecodeString(hash); err != nil {
			return Release{}, errors.New("magnet link has an invalid info hash")
		}
	case 32: // base32 form
		raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(hash))
		if err != nil {
			return Release{}, errors.New("magnet link has an invalid info hash")
		}
		hash = hex.EncodeToString(raw)
	default:
		return Release{}, errors.New("magnet link has no BitTorrent info hash")
	}
	name := q.Get("dn")
	if name == "" {
		name = "magnet " + hash[:12]
	}
	return Release{Source: "magnet", Name: name, InfoHash: hash, Magnet: link, Seeders: -2}, nil
}
