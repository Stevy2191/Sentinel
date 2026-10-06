package nettools

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CommonPorts is the "common" preset: the 100 TCP ports most often found
// open (nmap's top ports), ascending.
var CommonPorts = []int{
	7, 9, 13, 21, 22, 23, 25, 26, 37, 53,
	79, 80, 81, 88, 106, 110, 111, 113, 119, 135,
	139, 143, 144, 179, 199, 389, 427, 443, 444, 445,
	465, 513, 514, 515, 543, 544, 548, 554, 587, 631,
	646, 873, 990, 993, 995, 1025, 1026, 1027, 1028, 1029,
	1110, 1433, 1720, 1723, 1755, 1900, 2000, 2001, 2049, 2121,
	2717, 3000, 3128, 3306, 3389, 3986, 4899, 5000, 5009, 5051,
	5060, 5101, 5190, 5357, 5432, 5631, 5666, 5800, 5900, 6000,
	6001, 6646, 7070, 8000, 8008, 8009, 8080, 8081, 8443, 8888,
	9100, 9999, 10000, 32768, 49152, 49153, 49154, 49155, 49156, 49157,
}

// serviceNames are the well-known services shown beside a port.
var serviceNames = map[int]string{
	7: "echo", 9: "discard", 13: "daytime", 21: "ftp", 22: "ssh", 23: "telnet",
	25: "smtp", 37: "time", 53: "dns", 79: "finger", 80: "http", 88: "kerberos",
	110: "pop3", 111: "rpcbind", 113: "ident", 119: "nntp", 123: "ntp",
	135: "msrpc", 139: "netbios-ssn", 143: "imap", 161: "snmp", 179: "bgp",
	389: "ldap", 443: "https", 445: "smb", 465: "smtps", 514: "syslog",
	515: "printer", 548: "afp", 554: "rtsp", 587: "submission", 631: "ipp",
	636: "ldaps", 873: "rsync", 990: "ftps", 993: "imaps", 995: "pop3s",
	1433: "mssql", 1723: "pptp", 1900: "upnp", 2049: "nfs", 3000: "http-alt",
	3128: "squid", 3306: "mysql", 3389: "rdp", 5060: "sip", 5432: "postgresql",
	5900: "vnc", 5985: "winrm", 5986: "winrm-https", 6379: "redis",
	8000: "http-alt", 8080: "http-proxy", 8443: "https-alt", 9100: "jetdirect",
	9200: "elasticsearch", 27017: "mongodb",
}

// ServiceName is the well-known service on port ("ssh", "rdp", …) or "".
func ServiceName(port int) string { return serviceNames[port] }

var errTooManyPorts = fmt.Errorf("at most %d ports", MaxPorts)

// ParsePorts reads "common" or a comma-separated list of ports and ranges
// ("22,80,443,8000-8100"; spaces allowed). The result is ascending and
// de-duplicated; every port is 1–65535 and there are at most MaxPorts.
func ParsePorts(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, "common") {
		return append([]int(nil), CommonPorts...), nil
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		loText, hiText, isRange := strings.Cut(part, "-")
		lo, err := parsePort(loText)
		if err != nil {
			return nil, err
		}
		hi := lo
		if isRange {
			if hi, err = parsePort(hiText); err != nil {
				return nil, err
			}
			if lo > hi {
				return nil, fmt.Errorf("%q: the range starts above its end", part)
			}
		}
		for p := lo; p <= hi; p++ {
			seen[p] = true
			if len(seen) > MaxPorts {
				return nil, errTooManyPorts
			}
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("no ports given")
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports, nil
}

func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port number (1-65535)", s)
	}
	return n, nil
}
