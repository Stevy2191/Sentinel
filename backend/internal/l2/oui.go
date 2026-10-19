package l2

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strings"
	"sync"
)

// oui.tsv.gz is the IEEE MA-L registry as "<6 hex digits>\t<organisation>"
// lines, built from https://standards-oui.ieee.org/oui/oui.csv.
//
//go:embed oui.tsv.gz
var ouiData []byte

var (
	ouiOnce sync.Once
	ouiMap  map[string]string
)

func loadOUI() {
	ouiMap = map[string]string{}
	zr, err := gzip.NewReader(bytes.NewReader(ouiData))
	if err != nil {
		return
	}
	defer zr.Close()
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		if prefix, org, ok := strings.Cut(sc.Text(), "\t"); ok {
			ouiMap[prefix] = org
		}
	}
}

// Maker is the organisation the IEEE registered the MAC's first three
// bytes to, or "" (unknown, or a locally administered address such as a
// phone's private Wi-Fi MAC).
func Maker(mac string) string {
	norm, ok := NormalizeMAC(mac)
	if !ok {
		return ""
	}
	ouiOnce.Do(loadOUI)
	return ouiMap[strings.ReplaceAll(norm[:8], ":", "")]
}
