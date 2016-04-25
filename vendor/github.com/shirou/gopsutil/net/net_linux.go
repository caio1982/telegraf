// +build linux

package net

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/internal/common"
)

// NetIOCounters returnes network I/O statistics for every network
// interface installed on the system.  If pernic argument is false,
// return only sum of all information (which name is 'all'). If true,
// every network interface installed on the system is returned
// separately.
func NetIOCounters(pernic bool) ([]NetIOCountersStat, error) {
	filename := common.HostProc("net/dev")
	return NetIOCountersByFile(pernic, filename)
}

func NetIOCountersByFile(pernic bool, filename string) ([]NetIOCountersStat, error) {
	lines, err := common.ReadLines(filename)
	if err != nil {
		return nil, err
	}

	statlen := len(lines) - 1

	ret := make([]NetIOCountersStat, 0, statlen)

	for _, line := range lines[2:] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		interfaceName := strings.TrimSpace(parts[0])
		if interfaceName == "" {
			continue
		}

		fields := strings.Fields(strings.TrimSpace(parts[1]))
		bytesRecv, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return ret, err
		}
		packetsRecv, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return ret, err
		}
		errIn, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return ret, err
		}
		dropIn, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			return ret, err
		}
		bytesSent, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return ret, err
		}
		packetsSent, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return ret, err
		}
		errOut, err := strconv.ParseUint(fields[10], 10, 64)
		if err != nil {
			return ret, err
		}
		dropOut, err := strconv.ParseUint(fields[13], 10, 64)
		if err != nil {
			return ret, err
		}

		nic := NetIOCountersStat{
			Name:        interfaceName,
			BytesRecv:   bytesRecv,
			PacketsRecv: packetsRecv,
			Errin:       errIn,
			Dropin:      dropIn,
			BytesSent:   bytesSent,
			PacketsSent: packetsSent,
			Errout:      errOut,
			Dropout:     dropOut,
		}
		ret = append(ret, nic)
	}

	if pernic == false {
		return getNetIOCountersAll(ret)
	}

	return ret, nil
}

var netProtocols = []string{
	"ip",
	"icmp",
	"icmpmsg",
	"tcp",
	"udp",
	"udplite",
}

// NetProtoCounters returns network statistics for the entire system
// If protocols is empty then all protocols are returned, otherwise
// just the protocols in the list are returned.
// Available protocols:
//   ip,icmp,icmpmsg,tcp,udp,udplite
func NetProtoCounters(protocols []string) ([]NetProtoCountersStat, error) {
	if len(protocols) == 0 {
		protocols = netProtocols
	}

	stats := make([]NetProtoCountersStat, 0, len(protocols))
	protos := make(map[string]bool, len(protocols))
	for _, p := range protocols {
		protos[p] = true
	}

	filename := common.HostProc("net/snmp")
	lines, err := common.ReadLines(filename)
	if err != nil {
		return nil, err
	}

	linecount := len(lines)
	for i := 0; i < linecount; i++ {
		line := lines[i]
		r := strings.IndexRune(line, ':')
		if r == -1 {
			return nil, errors.New(filename + " is not fomatted correctly, expected ':'.")
		}
		proto := strings.ToLower(line[:r])
		if !protos[proto] {
			// skip protocol and data line
			i++
			continue
		}

		// Read header line
		statNames := strings.Split(line[r+2:], " ")

		// Read data line
		i++
		statValues := strings.Split(lines[i][r+2:], " ")
		if len(statNames) != len(statValues) {
			return nil, errors.New(filename + " is not fomatted correctly, expected same number of columns.")
		}
		stat := NetProtoCountersStat{
			Protocol: proto,
			Stats:    make(map[string]int64, len(statNames)),
		}
		for j := range statNames {
			value, err := strconv.ParseInt(statValues[j], 10, 64)
			if err != nil {
				return nil, err
			}
			stat.Stats[statNames[j]] = value
		}
		stats = append(stats, stat)
	}
	return stats, nil
}

// NetFilterCounters returns iptables conntrack statistics
// the currently in use conntrack count and the max.
// If the file does not exist or is invalid it will return nil.
func NetFilterCounters() ([]NetFilterStat, error) {
	countfile := common.HostProc("sys/net/netfilter/nf_conntrack_count")
	maxfile := common.HostProc("sys/net/netfilter/nf_conntrack_max")

	count, err := common.ReadInts(countfile)

	if err != nil {
		return nil, err
	}
	stats := make([]NetFilterStat, 0, 1)

	max, err := common.ReadInts(maxfile)
	if err != nil {
		return nil, err
	}

	payload := NetFilterStat{
		ConnTrackCount: count[0],
		ConnTrackMax:   max[0],
	}

	stats = append(stats, payload)
	return stats, nil
}

// http://students.mimuw.edu.pl/lxr/source/include/net/tcp_states.h
var TCPStatuses = map[string]string{
	"01": "ESTABLISHED",
	"02": "SYN_SENT",
	"03": "SYN_RECV",
	"04": "FIN_WAIT1",
	"05": "FIN_WAIT2",
	"06": "TIME_WAIT",
	"07": "CLOSE",
	"08": "CLOSE_WAIT",
	"09": "LAST_ACK",
	"0A": "LISTEN",
	"0B": "CLOSING",
}

type netConnectionKindType struct {
	family   uint32
	sockType uint32
	filename string
}

var kindTCP4 = netConnectionKindType{
	family:   syscall.AF_INET,
	sockType: syscall.SOCK_STREAM,
	filename: "tcp",
}
var kindTCP6 = netConnectionKindType{
	family:   syscall.AF_INET6,
	sockType: syscall.SOCK_STREAM,
	filename: "tcp6",
}
var kindUDP4 = netConnectionKindType{
	family:   syscall.AF_INET,
	sockType: syscall.SOCK_DGRAM,
	filename: "udp",
}
var kindUDP6 = netConnectionKindType{
	family:   syscall.AF_INET6,
	sockType: syscall.SOCK_DGRAM,
	filename: "udp6",
}
var kindUNIX = netConnectionKindType{
	family:   syscall.AF_UNIX,
	filename: "unix",
}

var netConnectionKindMap = map[string][]netConnectionKindType{
	"all":   []netConnectionKindType{kindTCP4, kindTCP6, kindUDP4, kindUDP6, kindUNIX},
	"tcp":   []netConnectionKindType{kindTCP4, kindTCP6},
	"tcp4":  []netConnectionKindType{kindTCP4},
	"tcp6":  []netConnectionKindType{kindTCP6},
	"udp":   []netConnectionKindType{kindUDP4, kindUDP6},
	"udp4":  []netConnectionKindType{kindUDP4},
	"udp6":  []netConnectionKindType{kindUDP6},
	"unix":  []netConnectionKindType{kindUNIX},
	"inet":  []netConnectionKindType{kindTCP4, kindTCP6, kindUDP4, kindUDP6},
	"inet4": []netConnectionKindType{kindTCP4, kindUDP4},
	"inet6": []netConnectionKindType{kindTCP6, kindUDP6},
}

type inodeMap struct {
	pid int32
	fd  uint32
}

type connTmp struct {
	fd       uint32
	family   uint32
	sockType uint32
	laddr    Addr
	raddr    Addr
	status   string
	pid      int32
	boundPid int32
	path     string
}

// Return a list of network connections opened.
func NetConnections(kind string) ([]NetConnectionStat, error) {
	return NetConnectionsPid(kind, 0)
}

// Return a list of network connections opened by a process.
func NetConnectionsPid(kind string, pid int32) ([]NetConnectionStat, error) {
	tmap, ok := netConnectionKindMap[kind]
	if !ok {
		return nil, fmt.Errorf("invalid kind, %s", kind)
	}
	root := common.HostProc()
	var err error
	var inodes map[string][]inodeMap
	if pid == 0 {
		inodes, err = getProcInodesAll(root)
	} else {
		inodes, err = getProcInodes(root, pid)
		if len(inodes) == 0 {
			// no connection for the pid
			return []NetConnectionStat{}, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("cound not get pid(s), %d", pid)
	}

	dupCheckMap := make(map[string]bool)
	var ret []NetConnectionStat

	for _, t := range tmap {
		var path string
		var ls []connTmp
		path = fmt.Sprintf("%s/net/%s", root, t.filename)
		switch t.family {
		case syscall.AF_INET:
			fallthrough
		case syscall.AF_INET6:
			ls, err = processInet(path, t, inodes, pid)
		case syscall.AF_UNIX:
			ls, err = processUnix(path, t, inodes, pid)
		}
		if err != nil {
			return nil, err
		}
		for _, c := range ls {
			conn := NetConnectionStat{
				Fd:     c.fd,
				Family: c.family,
				Type:   c.sockType,
				Laddr:  c.laddr,
				Raddr:  c.raddr,
				Status: c.status,
				Pid:    c.pid,
			}
			if c.pid == 0 {
				conn.Pid = c.boundPid
			} else {
				conn.Pid = c.pid
			}
			// check duplicate using JSON format
			json := conn.String()
			_, exists := dupCheckMap[json]
			if !exists {
				ret = append(ret, conn)
				dupCheckMap[json] = true
			}
		}

	}

	return ret, nil
}

// getProcInodes returnes fd of the pid.
func getProcInodes(root string, pid int32) (map[string][]inodeMap, error) {
	ret := make(map[string][]inodeMap)

	dir := fmt.Sprintf("%s/%d/fd", root, pid)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return ret, nil
	}
	for _, fd := range files {
		inodePath := fmt.Sprintf("%s/%d/fd/%s", root, pid, fd.Name())

		inode, err := os.Readlink(inodePath)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(inode, "socket:[") {
			continue
		}
		// the process is using a socket
		l := len(inode)
		inode = inode[8 : l-1]
		_, ok := ret[inode]
		if !ok {
			ret[inode] = make([]inodeMap, 0)
		}
		fd, err := strconv.Atoi(fd.Name())
		if err != nil {
			continue
		}

		i := inodeMap{
			pid: pid,
			fd:  uint32(fd),
		}
		ret[inode] = append(ret[inode], i)
	}
	return ret, nil
}

// Pids retunres all pids.
// Note: this is a copy of process_linux.Pids()
// FIXME: Import process occures import cycle.
// move to common made other platform breaking. Need consider.
func Pids() ([]int32, error) {
	var ret []int32

	d, err := os.Open(common.HostProc())
	if err != nil {
		return nil, err
	}
	defer d.Close()

	fnames, err := d.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	for _, fname := range fnames {
		pid, err := strconv.ParseInt(fname, 10, 32)
		if err != nil {
			// if not numeric name, just skip
			continue
		}
		ret = append(ret, int32(pid))
	}

	return ret, nil
}

func getProcInodesAll(root string) (map[string][]inodeMap, error) {
	pids, err := Pids()
	if err != nil {
		return nil, err
	}
	ret := make(map[string][]inodeMap)

	for _, pid := range pids {
		t, err := getProcInodes(root, pid)
		if err != nil {
			return ret, err
		}
		if len(t) == 0 {
			continue
		}
		// TODO: update ret.
		ret = updateMap(ret, t)
	}
	return ret, nil
}

// decodeAddress decode addresse represents addr in proc/net/*
// ex:
// "0500000A:0016" -> "10.0.0.5", 22
// "0085002452100113070057A13F025401:0035" -> "2400:8500:1301:1052:a157:7:154:23f", 53
func decodeAddress(family uint32, src string) (Addr, error) {
	t := strings.Split(src, ":")
	if len(t) != 2 {
		return Addr{}, fmt.Errorf("does not contain port, %s", src)
	}
	addr := t[0]
	port, err := strconv.ParseInt("0x"+t[1], 0, 64)
	if err != nil {
		return Addr{}, fmt.Errorf("invalid port, %s", src)
	}
	decoded, err := hex.DecodeString(addr)
	if err != nil {
		return Addr{}, fmt.Errorf("decode error, %s", err)
	}
	var ip net.IP
	// Assumes this is little_endian
	if family == syscall.AF_INET {
		ip = net.IP(Reverse(decoded))
	} else { // IPv6
		ip, err = parseIPv6HexString(decoded)
		if err != nil {
			return Addr{}, err
		}
	}
	return Addr{
		IP:   ip.String(),
		Port: uint32(port),
	}, nil
}

// Reverse reverses array of bytes.
func Reverse(s []byte) []byte {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// parseIPv6HexString parse array of bytes to IPv6 string
func parseIPv6HexString(src []byte) (net.IP, error) {
	if len(src) != 16 {
		return nil, fmt.Errorf("invalid IPv6 string")
	}

	buf := make([]byte, 0, 16)
	for i := 0; i < len(src); i += 4 {
		r := Reverse(src[i : i+4])
		buf = append(buf, r...)
	}
	return net.IP(buf), nil
}

func processInet(file string, kind netConnectionKindType, inodes map[string][]inodeMap, filterPid int32) ([]connTmp, error) {

	if strings.HasSuffix(file, "6") && !common.PathExists(file) {
		// IPv6 not supported, return empty.
		return []connTmp{}, nil
	}
	lines, err := common.ReadLines(file)
	if err != nil {
		return nil, err
	}
	var ret []connTmp
	// skip first line
	for _, line := range lines[1:] {
		l := strings.Fields(line)
		if len(l) < 10 {
			continue
		}
		laddr := l[1]
		raddr := l[2]
		status := l[3]
		inode := l[9]
		pid := int32(0)
		fd := uint32(0)
		i, exists := inodes[inode]
		if exists {
			pid = i[0].pid
			fd = i[0].fd
		}
		if filterPid > 0 && filterPid != pid {
			continue
		}
		if kind.sockType == syscall.SOCK_STREAM {
			status = TCPStatuses[status]
		} else {
			status = "NONE"
		}
		la, err := decodeAddress(kind.family, laddr)
		if err != nil {
			continue
		}
		ra, err := decodeAddress(kind.family, raddr)
		if err != nil {
			continue
		}

		ret = append(ret, connTmp{
			fd:       fd,
			family:   kind.family,
			sockType: kind.sockType,
			laddr:    la,
			raddr:    ra,
			status:   status,
			pid:      pid,
		})
	}

	return ret, nil
}

func processUnix(file string, kind netConnectionKindType, inodes map[string][]inodeMap, filterPid int32) ([]connTmp, error) {
	lines, err := common.ReadLines(file)
	if err != nil {
		return nil, err
	}

	var ret []connTmp
	// skip first line
	for _, line := range lines[1:] {
		tokens := strings.Fields(line)
		if len(tokens) < 6 {
			continue
		}
		st, err := strconv.Atoi(tokens[4])
		if err != nil {
			return nil, err
		}

		inode := tokens[6]

		var pairs []inodeMap
		pairs, exists := inodes[inode]
		if !exists {
			pairs = []inodeMap{
				inodeMap{},
			}
		}
		for _, pair := range pairs {
			if filterPid > 0 && filterPid != pair.pid {
				continue
			}
			var path string
			if len(tokens) == 8 {
				path = tokens[len(tokens)-1]
			}
			ret = append(ret, connTmp{
				fd:       pair.fd,
				family:   kind.family,
				sockType: uint32(st),
				laddr: Addr{
					IP: path,
				},
				pid:    pair.pid,
				status: "NONE",
				path:   path,
			})
		}
	}

	return ret, nil
}

func updateMap(src map[string][]inodeMap, add map[string][]inodeMap) map[string][]inodeMap {
	for key, value := range add {
		a, exists := src[key]
		if !exists {
			src[key] = value
			continue
		}
		src[key] = append(a, value...)
	}
	return src
}
