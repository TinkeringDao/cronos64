package thermal

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Config configures the userspace thermal guard.
type Config struct {
	// ThermalRoot is the base sysfs directory for thermal zones (default "/sys/class/thermal").
	ThermalRoot string
	// CpufreqLimitPath is the primary userspace limit path on MT8163 4.9
	// ("/proc/cpufreq/cpufreq_limited_max_freq_by_user").
	CpufreqLimitPath string
	// FallbackScalingMaxPath is the fallback cpufreq limit path
	// ("/sys/devices/system/cpu/cpufreq/policy0/scaling_max_freq").
	FallbackScalingMaxPath string
	// HighMilliC is the high threshold in m°C where frequency steps down (e.g. 80000).
	HighMilliC int
	// RestoreMilliC is the restore threshold in m°C where frequency steps up (e.g. 75000).
	RestoreMilliC int
	// OPPs are descending operating performance points in kHz.
	OPPs []int
	// FailSafeKHz is the safe frequency in kHz clamped to on sensor error (e.g. 1050000).
	FailSafeKHz int
}

// DefaultCronosConfig returns the default thermal guard configuration for Echo Show 5 (cronos)
// with FY-binned MT8163 silicon.
func DefaultCronosConfig() Config {
	return Config{
		ThermalRoot:            "/sys/class/thermal",
		CpufreqLimitPath:       "/proc/cpufreq/cpufreq_limited_max_freq_by_user",
		FallbackScalingMaxPath: "/sys/devices/system/cpu/cpufreq/policy0/scaling_max_freq",
		HighMilliC:             80000,
		RestoreMilliC:          75000,
		OPPs:                   []int{1300000, 1216000, 1133000, 1050000, 871000},
		FailSafeKHz:            1050000,
	}
}

// Snapshot captures the current thermal status and enforced limit.
type Snapshot struct {
	TempMilliC int
	LimitKHz   int
	Degraded   bool
}

// Guard supervises CPU thermal zones and clamps cpufreq using userspace hysteresis.
type Guard struct {
	mu       sync.Mutex
	cfg      Config
	oppIndex int
	zonePath string
}

// New creates a new thermal guard without performing any hardware side effects or sysfs reads.
func New(cfg Config) *Guard {
	defaults := DefaultCronosConfig()
	if cfg.ThermalRoot == "" {
		cfg.ThermalRoot = defaults.ThermalRoot
	}
	if cfg.CpufreqLimitPath == "" && cfg.FallbackScalingMaxPath == "" {
		cfg.CpufreqLimitPath = defaults.CpufreqLimitPath
		cfg.FallbackScalingMaxPath = defaults.FallbackScalingMaxPath
	}
	if cfg.HighMilliC == 0 {
		cfg.HighMilliC = defaults.HighMilliC
	}
	if cfg.RestoreMilliC == 0 {
		cfg.RestoreMilliC = defaults.RestoreMilliC
	}
	if len(cfg.OPPs) == 0 {
		cfg.OPPs = append([]int(nil), defaults.OPPs...)
	}
	if cfg.FailSafeKHz == 0 {
		cfg.FailSafeKHz = defaults.FailSafeKHz
	}

	return &Guard{
		cfg:      cfg,
		oppIndex: 0,
	}
}

// NewGuard is an alias for New.
func NewGuard(cfg Config) *Guard {
	return New(cfg)
}

// zoneNumber parses the trailing integer from thermal_zoneN for stable sorting.
func zoneNumber(path string) int {
	base := filepath.Base(path)
	n, err := strconv.Atoi(strings.TrimPrefix(base, "thermal_zone"))
	if err != nil {
		return math.MaxInt
	}
	return n
}

// discoverZone finds the path to the mtktscpu temp file lazily.
func (g *Guard) discoverZone() (string, error) {
	if g.zonePath != "" {
		return g.zonePath, nil
	}

	root := g.cfg.ThermalRoot
	if root == "" {
		root = "/sys/class/thermal"
	}

	matches, err := filepath.Glob(filepath.Join(root, "thermal_zone*"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("thermal: no zones found in %s", root)
	}

	// Sort numerically so thermal_zone2 precedes thermal_zone12
	slices.SortFunc(matches, func(a, b string) int {
		return cmp.Compare(zoneNumber(a), zoneNumber(b))
	})

	for _, z := range matches {
		typeBytes, err := os.ReadFile(filepath.Join(z, "type"))
		if err != nil {
			continue
		}
		typeName := strings.TrimSpace(string(typeBytes))
		if typeName == "mtktscpu" || typeName == "cpu-thermal" {
			g.zonePath = filepath.Join(z, "temp")
			return g.zonePath, nil
		}
	}

	return "", fmt.Errorf("thermal: mtktscpu zone not found in %s", root)
}

// writeLimit persists the frequency limit in kHz to the available cpufreq sink.
func (g *Guard) writeLimit(limitKHz int) error {
	val := fmt.Sprintf("%d\n", limitKHz)

	if g.cfg.CpufreqLimitPath != "" {
		err := os.WriteFile(g.cfg.CpufreqLimitPath, []byte(val), 0644)
		if err == nil {
			return nil
		}
		// If primary path does not exist, attempt fallback
		if g.cfg.FallbackScalingMaxPath != "" && (os.IsNotExist(err) || errors.Is(err, os.ErrNotExist)) {
			if fbErr := os.WriteFile(g.cfg.FallbackScalingMaxPath, []byte(val), 0644); fbErr == nil {
				return nil
			}
		}
		return err
	}

	if g.cfg.FallbackScalingMaxPath != "" {
		return os.WriteFile(g.cfg.FallbackScalingMaxPath, []byte(val), 0644)
	}

	return errors.New("thermal: no cpufreq limit path configured")
}

// setOppByLimit sets the current OPP index to match or exceed the requested limit in kHz.
func (g *Guard) setOppByLimit(limitKHz int) {
	for i, opp := range g.cfg.OPPs {
		if opp <= limitKHz {
			g.oppIndex = i
			return
		}
	}
	g.oppIndex = len(g.cfg.OPPs) - 1
}

// Step reads the CPU thermal zone, applies hysteresis to step OPPs, and persists the limit.
func (g *Guard) Step() (Snapshot, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tempPath, err := g.discoverZone()
	if err != nil {
		g.zonePath = ""
		g.setOppByLimit(g.cfg.FailSafeKHz)
		snap := Snapshot{
			TempMilliC: 0,
			LimitKHz:   g.cfg.FailSafeKHz,
			Degraded:   true,
		}
		writeErr := g.writeLimit(g.cfg.FailSafeKHz)
		return snap, writeErr
	}

	tempBytes, err := os.ReadFile(tempPath)
	if err != nil {
		g.zonePath = ""
		g.setOppByLimit(g.cfg.FailSafeKHz)
		snap := Snapshot{
			TempMilliC: 0,
			LimitKHz:   g.cfg.FailSafeKHz,
			Degraded:   true,
		}
		writeErr := g.writeLimit(g.cfg.FailSafeKHz)
		return snap, writeErr
	}

	temp, err := strconv.Atoi(strings.TrimSpace(string(tempBytes)))
	if err != nil || temp <= 0 {
		g.zonePath = ""
		g.setOppByLimit(g.cfg.FailSafeKHz)
		snap := Snapshot{
			TempMilliC: temp,
			LimitKHz:   g.cfg.FailSafeKHz,
			Degraded:   true,
		}
		writeErr := g.writeLimit(g.cfg.FailSafeKHz)
		return snap, writeErr
	}

	// Healthy sensor reading: apply hysteresis across descending OPPs
	if temp >= g.cfg.HighMilliC {
		if g.oppIndex < len(g.cfg.OPPs)-1 {
			g.oppIndex++
		}
	} else if temp < g.cfg.RestoreMilliC {
		if g.oppIndex > 0 {
			g.oppIndex--
		}
	}
	// Within [RestoreMilliC, HighMilliC): hysteresis hold

	limitKHz := g.cfg.OPPs[g.oppIndex]
	snap := Snapshot{
		TempMilliC: temp,
		LimitKHz:   limitKHz,
		Degraded:   false,
	}

	writeErr := g.writeLimit(limitKHz)
	return snap, writeErr
}
