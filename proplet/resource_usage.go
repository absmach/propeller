package proplet

import (
	"bufio"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type usageCollector struct {
	mu sync.Mutex

	prevProcJiffies  uint64
	prevTotalJiffies uint64
}

func newUsageCollector() *usageCollector {
	return &usageCollector{}
}

func (c *usageCollector) Collect() Metrics {
	now := time.Now()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	cpu := CPUMetrics{}
	mem := MemoryMetrics{
		HeapAllocBytes: ms.HeapAlloc,
		HeapSysBytes:   ms.HeapSys,
		HeapInuseBytes: ms.HeapInuse,
	}

	if ut, st, rss, ok := readProcSelfStat(); ok {
		const hz = 100.0
		cpu.UserSeconds = float64(ut) / hz
		cpu.SystemSeconds = float64(st) / hz
		mem.RSSBytes = rss
	}

	if usage, limit, ok := readCgroupMemory(); ok {
		mem.ContainerUsageBytes = &usage
		if limit > 0 {
			mem.ContainerLimitBytes = &limit
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if procJ, totalJ, ok := readProcCPUJiffies(); ok {
		if c.prevTotalJiffies > 0 && totalJ > c.prevTotalJiffies {
			procDelta := float64(procJ - c.prevProcJiffies)
			totalDelta := float64(totalJ - c.prevTotalJiffies)
			cpu.Percent = (procDelta / totalDelta) * 100.0
		}
		c.prevProcJiffies = procJ
		c.prevTotalJiffies = totalJ
	}

	return Metrics{
		Version:   "v1",
		Timestamp: now,
		CPU:       cpu,
		Memory:    mem,
	}
}

func readProcSelfStat() (utime, stime, rssBytes uint64, ok bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return
	}
	s := string(b)
	rp := strings.LastIndexByte(s, ')')
	if rp < 0 {
		return
	}
	fields := strings.Fields(s[rp+2:])
	if len(fields) < 22 {
		return
	}

	utime, _ = strconv.ParseUint(fields[11], 10, 64)
	stime, _ = strconv.ParseUint(fields[12], 10, 64)
	rssPages, _ := strconv.ParseInt(fields[21], 10, 64)

	return utime, stime, uint64(rssPages) * uint64(os.Getpagesize()), true
}

func readProcCPUJiffies() (procJiffies, totalJiffies uint64, ok bool) {
	ut, st, _, ok1 := readProcSelfStat()
	if !ok1 {
		return 0, 0, false
	}
	total, ok2 := readProcStatTotalJiffies()
	if !ok2 {
		return 0, 0, false
	}

	return ut + st, total, true
}

func readProcStatTotalJiffies() (total uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "cpu ") {
			parts := strings.Fields(sc.Text())[1:]
			var sum uint64
			for _, p := range parts {
				v, err := strconv.ParseUint(p, 10, 64)
				if err != nil {
					return 0, false
				}
				sum += v
			}

			return sum, true
		}
	}

	return 0, false
}

func readCgroupMemory() (usage, limit uint64, ok bool) {
	// cgroup v2.
	if u, err := readUintStrict("/sys/fs/cgroup/memory.current"); err == nil {
		if lim, ok := readCgroupV2Limit("/sys/fs/cgroup/memory.max"); ok {
			return u, lim, true
		}

		return u, 0, true
	}

	// cgroup v1.
	u, errU := readUintStrict("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	lim, errL := readUintStrict("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	if errU == nil && errL == nil {
		return u, lim, true
	}
	if errU == nil {
		return u, 0, true
	}

	return 0, 0, false
}

func readCgroupV2Limit(path string) (limit uint64, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "" || s == "max" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}

	return v, true
}

func readUintStrict(path string) (value uint64, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, errors.New("empty value")
	}

	return strconv.ParseUint(s, 10, 64)
}
