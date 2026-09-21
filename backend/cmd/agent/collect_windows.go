package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// None of GetSystemTimes, GlobalMemoryStatusEx or GetTickCount64 are exported
// by golang.org/x/sys/windows (GetTickCount64 exists there only as a
// lowercase, package-private wrapper), so they are called directly the same
// way that package's own generated wrappers call into kernel32.
var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes       = modkernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64       = modkernel32.NewProc("GetTickCount64")
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX struct, which
// golang.org/x/sys/windows does not define.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func getSystemTimes() (idle, kernel, user windows.Filetime, err error) {
	r1, _, e1 := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		return idle, kernel, user, e1
	}
	return idle, kernel, user, nil
}

func globalMemoryStatusEx() (memoryStatusEx, error) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	r1, _, e1 := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if r1 == 0 {
		return status, e1
	}
	return status, nil
}

// getTickCount64 never fails per its Win32 documentation.
func getTickCount64() uint64 {
	r1, _, _ := procGetTickCount64.Call()
	return uint64(r1)
}

// Collector gathers host metrics. It holds the previous CPU sample because
// utilisation is a rate: GetSystemTimes reports cumulative time since boot,
// and a single reading gives the average since boot rather than the load now.
type Collector struct {
	prevIdle, prevTotal uint64
	havePrev            bool
	diskPath            string
}

func NewCollector(diskPath string) *Collector {
	if diskPath == "" {
		diskPath = `C:\`
	}
	return &Collector{diskPath: diskPath}
}

// Collect reads one cycle. Individual readings that fail are left unset
// rather than failing the cycle, the same contract collect_linux.go follows.
//
// Load average is never set: Windows has no equivalent of a Unix run-queue
// average, and fabricating one would be worse than omitting it.
func (c *Collector) Collect() Metrics {
	m := Metrics{Timestamp: time.Now().UTC(), Containers: []Container{}}

	if pct, err := c.cpuPercent(); err == nil {
		m.CPUPercent = &pct
	}
	if used, total, pct, err := memoryUsage(); err == nil {
		m.MemoryUsedMB, m.MemoryTotalMB, m.MemoryPercent = &used, &total, &pct
	}
	if used, total, pct, err := diskUsage(c.diskPath); err == nil {
		m.DiskUsedGB, m.DiskTotalGB, m.DiskPercent = &used, &total, &pct
	}
	if up, err := uptimeSeconds(); err == nil {
		m.UptimeSeconds = &up
	}
	if in, out, err := network(); err == nil {
		m.NetworkInBytes, m.NetworkOutBytes = &in, &out
	}
	return m
}

// cpuPercent returns utilisation since the previous call, mirroring
// collect_linux.go's idle/total delta approach. GetSystemTimes' "kernel" time
// already includes idle time, so total is kernel+user, not idle+kernel+user.
func (c *Collector) cpuPercent() (float64, error) {
	idleFT, kernelFT, userFT, err := getSystemTimes()
	if err != nil {
		return 0, err
	}
	idle := filetimeToUint64(idleFT)
	total := filetimeToUint64(kernelFT) + filetimeToUint64(userFT)

	prevIdle, prevTotal, havePrev := c.prevIdle, c.prevTotal, c.havePrev
	c.prevIdle, c.prevTotal, c.havePrev = idle, total, true
	if !havePrev {
		return 0, fmt.Errorf("no previous sample yet")
	}

	totalDelta := float64(total - prevTotal)
	idleDelta := float64(idle - prevIdle)
	if totalDelta <= 0 {
		return 0, fmt.Errorf("no elapsed CPU time")
	}
	pct := (totalDelta - idleDelta) / totalDelta * 100
	return clampPercent(pct), nil
}

func filetimeToUint64(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

func clampPercent(pct float64) float64 {
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// memoryUsage reports used/total/percent, in MB and as a fraction, the same
// shape hoststats.Memory returns on Linux.
func memoryUsage() (usedMB, totalMB int64, percent float64, err error) {
	status, err := globalMemoryStatusEx()
	if err != nil {
		return 0, 0, 0, err
	}
	totalMB = int64(status.TotalPhys / (1024 * 1024))
	usedBytes := status.TotalPhys - status.AvailPhys
	usedMB = int64(usedBytes / (1024 * 1024))
	if status.TotalPhys > 0 {
		percent = clampPercent(float64(usedBytes) / float64(status.TotalPhys) * 100)
	}
	return usedMB, totalMB, percent, nil
}

func memoryTotalMB() (int64, error) {
	_, total, _, err := memoryUsage()
	return total, err
}

// diskUsage reports used/total/percent, in GB, for the drive diskPath is on.
func diskUsage(diskPath string) (usedGB, totalGB, percent float64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(diskPath)
	if err != nil {
		return 0, 0, 0, err
	}
	var freeAvail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeAvail, &total, &free); err != nil {
		return 0, 0, 0, err
	}
	const gb = 1024 * 1024 * 1024
	totalGB = float64(total) / gb
	usedBytes := total - free
	usedGB = float64(usedBytes) / gb
	if total > 0 {
		percent = clampPercent(float64(usedBytes) / float64(total) * 100)
	}
	return usedGB, totalGB, percent, nil
}

// uptimeSeconds reads milliseconds since boot via GetTickCount64, which does
// not wrap for about 584 years — unlike the 32-bit GetTickCount, which wraps
// after 49.7 days.
func uptimeSeconds() (int64, error) {
	return int64(getTickCount64() / 1000), nil
}

// network sums bytes in and out across real adapters, mirroring
// collect_linux.go's exclusion of loopback and virtual interfaces.
func network() (in, out int64, err error) {
	rows, err := interfaceRows()
	if err != nil {
		return 0, 0, err
	}
	for _, row := range rows {
		if row.Type == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}
		if isVirtualInterface(windows.UTF16ToString(row.Description[:])) {
			continue
		}
		in += int64(row.InOctets)
		out += int64(row.OutOctets)
	}
	return in, out, nil
}

// interfaceRows fetches the adapter table. MibIfTable2's Table field is
// declared as a 1-element array standing in for a variable-length one (the
// same C ANYSIZE_ARRAY convention GetIfTable2Ex's caller is expected to know
// about), so entries beyond the first are reached with pointer arithmetic
// rather than ordinary indexing.
func interfaceRows() ([]windows.MibIfRow2, error) {
	var table *windows.MibIfTable2
	if err := windows.GetIfTable2Ex(windows.MibIfTableNormal, &table); err != nil {
		return nil, err
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	rowSize := unsafe.Sizeof(table.Table[0])
	first := unsafe.Pointer(&table.Table[0])
	rows := make([]windows.MibIfRow2, 0, table.NumEntries)
	for i := uint32(0); i < table.NumEntries; i++ {
		row := (*windows.MibIfRow2)(unsafe.Pointer(uintptr(first) + uintptr(i)*rowSize))
		rows = append(rows, *row)
	}
	return rows, nil
}

// isVirtualInterface excludes adapters that are not a physical path off the
// machine: virtualization switches, tunnels and loopback pseudo-interfaces.
// A heuristic, the same spirit as collect_linux.go's prefix filter — Windows
// has no equivalent of a kernel-enforced naming convention for these either.
func isVirtualInterface(description string) bool {
	d := strings.ToLower(description)
	for _, needle := range []string{
		"virtual", "loopback", "vethernet", "hyper-v", "wsl",
		"tap-windows", "tunnel", "teredo", "isatap", "vmware", "virtualbox",
		"npcap",
	} {
		if strings.Contains(d, needle) {
			return true
		}
	}
	return false
}

// hostname reports the host's name. No host-mount indirection is needed the
// way Linux's Docker install path requires: the Windows agent always runs
// directly on the host, never inside a container reading a mounted /etc.
func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// osVersion and kernelVersion read from the registry rather than shelling out
// to systeminfo.exe (several seconds per call) or wmic (deprecated).
const currentVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

func osVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, currentVersionKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	product, _, _ := k.GetStringValue("ProductName")
	display, _, dispErr := k.GetStringValue("DisplayVersion")
	if dispErr != nil {
		// Older builds (pre-2004) never had DisplayVersion; ReleaseId is the
		// equivalent field there.
		display, _, _ = k.GetStringValue("ReleaseId")
	}
	if product != "" && display != "" {
		return product + " " + display
	}
	return product
}

func kernelVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, currentVersionKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	major, _, majErr := k.GetIntegerValue("CurrentMajorVersionNumber")
	minor, _, minErr := k.GetIntegerValue("CurrentMinorVersionNumber")
	build, _, buildErr := k.GetStringValue("CurrentBuildNumber")
	if majErr != nil || minErr != nil || buildErr != nil {
		return build
	}
	ubr, _, ubrErr := k.GetIntegerValue("UBR")
	if ubrErr != nil {
		return fmt.Sprintf("%d.%d.%s", major, minor, build)
	}
	return fmt.Sprintf("%d.%d.%s.%d", major, minor, build, ubr)
}

// cpuInfo returns the processor model and how many cores the host has.
// runtime.NumCPU is accurate here: Windows has nothing equivalent to a
// container CPU-share limit changing what a process sees.
func cpuInfo() (model string, cores int) {
	cores = runtime.NumCPU()
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return "", cores
	}
	defer k.Close()

	name, _, err := k.GetStringValue("ProcessorNameString")
	if err != nil {
		return "", cores
	}
	return strings.TrimSpace(name), cores
}
