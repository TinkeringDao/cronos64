package thermal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGuardStepsDownAt80CAndRestoresBelow75C(t *testing.T) {
	tmp := t.TempDir()
	zoneDir := filepath.Join(tmp, "sys/class/thermal/thermal_zone2")
	if err := os.MkdirAll(zoneDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zoneDir, "type"), []byte("mtktscpu\n"), 0644); err != nil {
		t.Fatal(err)
	}

	procDir := filepath.Join(tmp, "proc/cpufreq")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	limitPath := filepath.Join(procDir, "cpufreq_limited_max_freq_by_user")

	cfg := DefaultCronosConfig()
	cfg.ThermalRoot = filepath.Join(tmp, "sys/class/thermal")
	cfg.CpufreqLimitPath = limitPath

	g := New(cfg)

	// Feed 72000 -> 81000 -> 82000 -> 77000 -> 74000 -> 70000 m°C
	// and assert cpufreq_limited_max_freq_by_user transitions:
	// 1300000 -> 1216000 -> 1133000 -> 1133000 (hysteresis hold) -> 1216000 -> 1300000.
	steps := []struct {
		tempMilliC int
		wantLimit  int
	}{
		{72000, 1300000},
		{81000, 1216000},
		{82000, 1133000},
		{77000, 1133000}, // hysteresis hold in [75000, 80000)
		{74000, 1216000},
		{70000, 1300000},
	}

	tempFile := filepath.Join(zoneDir, "temp")
	for i, step := range steps {
		if err := os.WriteFile(tempFile, []byte(strconv.Itoa(step.tempMilliC)+"\n"), 0644); err != nil {
			t.Fatalf("step %d write temp: %v", i, err)
		}

		snap, err := g.Step()
		if err != nil {
			t.Fatalf("step %d Step() error: %v", i, err)
		}
		if snap.Degraded {
			t.Fatalf("step %d unexpected degraded state", i)
		}
		if snap.TempMilliC != step.tempMilliC {
			t.Fatalf("step %d TempMilliC = %d, want %d", i, snap.TempMilliC, step.tempMilliC)
		}
		if snap.LimitKHz != step.wantLimit {
			t.Fatalf("step %d LimitKHz = %d, want %d", i, snap.LimitKHz, step.wantLimit)
		}

		limitBytes, err := os.ReadFile(limitPath)
		if err != nil {
			t.Fatalf("step %d read limit file: %v", i, err)
		}
		limitStr := strings.TrimSpace(string(limitBytes))
		if limitStr != strconv.Itoa(step.wantLimit) {
			t.Fatalf("step %d limit file = %q, want %q", i, limitStr, strconv.Itoa(step.wantLimit))
		}
	}
}

func TestGuardClampsTo1050MHzOnSensorFailure(t *testing.T) {
	t.Run("MissingZone", func(t *testing.T) {
		tmp := t.TempDir()
		thermalDir := filepath.Join(tmp, "sys/class/thermal")
		if err := os.MkdirAll(thermalDir, 0755); err != nil {
			t.Fatal(err)
		}
		// Create another unrelated zone so thermal directory isn't empty, but mtktscpu is missing
		otherZone := filepath.Join(thermalDir, "thermal_zone0")
		if err := os.MkdirAll(otherZone, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(otherZone, "type"), []byte("mtktswmt\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(otherZone, "temp"), []byte("45000\n"), 0644); err != nil {
			t.Fatal(err)
		}

		procDir := filepath.Join(tmp, "proc/cpufreq")
		if err := os.MkdirAll(procDir, 0755); err != nil {
			t.Fatal(err)
		}
		limitPath := filepath.Join(procDir, "cpufreq_limited_max_freq_by_user")

		cfg := DefaultCronosConfig()
		cfg.ThermalRoot = thermalDir
		cfg.CpufreqLimitPath = limitPath

		g := New(cfg)
		snap, err := g.Step()
		if err != nil {
			t.Fatalf("Step() error: %v", err)
		}
		if !snap.Degraded {
			t.Errorf("snap.Degraded = false, want true")
		}
		if snap.LimitKHz != 1050000 {
			t.Errorf("snap.LimitKHz = %d, want 1050000", snap.LimitKHz)
		}

		limitBytes, err := os.ReadFile(limitPath)
		if err != nil {
			t.Fatalf("read limit file: %v", err)
		}
		if string(limitBytes) != "1050000\n" {
			t.Errorf("limit file content = %q, want \"1050000\\n\"", string(limitBytes))
		}
	})

	t.Run("CorruptTemp", func(t *testing.T) {
		tmp := t.TempDir()
		zoneDir := filepath.Join(tmp, "sys/class/thermal/thermal_zone1")
		if err := os.MkdirAll(zoneDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(zoneDir, "type"), []byte("mtktscpu\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(zoneDir, "temp"), []byte("corrupt-garbage\n"), 0644); err != nil {
			t.Fatal(err)
		}

		procDir := filepath.Join(tmp, "proc/cpufreq")
		if err := os.MkdirAll(procDir, 0755); err != nil {
			t.Fatal(err)
		}
		limitPath := filepath.Join(procDir, "cpufreq_limited_max_freq_by_user")

		cfg := DefaultCronosConfig()
		cfg.ThermalRoot = filepath.Join(tmp, "sys/class/thermal")
		cfg.CpufreqLimitPath = limitPath

		g := New(cfg)
		snap, err := g.Step()
		if err != nil {
			t.Fatalf("Step() error: %v", err)
		}
		if !snap.Degraded {
			t.Errorf("snap.Degraded = false, want true")
		}
		if snap.LimitKHz != 1050000 {
			t.Errorf("snap.LimitKHz = %d, want 1050000", snap.LimitKHz)
		}

		limitBytes, err := os.ReadFile(limitPath)
		if err != nil {
			t.Fatalf("read limit file: %v", err)
		}
		if string(limitBytes) != "1050000\n" {
			t.Errorf("limit file content = %q, want \"1050000\\n\"", string(limitBytes))
		}
	})

	t.Run("NonPositiveTemp", func(t *testing.T) {
		tmp := t.TempDir()
		zoneDir := filepath.Join(tmp, "sys/class/thermal/thermal_zone1")
		if err := os.MkdirAll(zoneDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(zoneDir, "type"), []byte("mtktscpu\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(zoneDir, "temp"), []byte("0\n"), 0644); err != nil {
			t.Fatal(err)
		}

		procDir := filepath.Join(tmp, "proc/cpufreq")
		if err := os.MkdirAll(procDir, 0755); err != nil {
			t.Fatal(err)
		}
		limitPath := filepath.Join(procDir, "cpufreq_limited_max_freq_by_user")

		cfg := DefaultCronosConfig()
		cfg.ThermalRoot = filepath.Join(tmp, "sys/class/thermal")
		cfg.CpufreqLimitPath = limitPath

		g := New(cfg)
		snap, err := g.Step()
		if err != nil {
			t.Fatalf("Step() error: %v", err)
		}
		if !snap.Degraded {
			t.Errorf("snap.Degraded = false, want true")
		}
		if snap.LimitKHz != 1050000 {
			t.Errorf("snap.LimitKHz = %d, want 1050000", snap.LimitKHz)
		}
	})
}

func TestGuardFallbackScalingMaxPath(t *testing.T) {
	tmp := t.TempDir()
	zoneDir := filepath.Join(tmp, "sys/class/thermal/thermal_zone0")
	if err := os.MkdirAll(zoneDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zoneDir, "type"), []byte("mtktscpu\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zoneDir, "temp"), []byte("70000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fallbackDir := filepath.Join(tmp, "sys/devices/system/cpu/cpufreq/policy0")
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		t.Fatal(err)
	}
	fallbackPath := filepath.Join(fallbackDir, "scaling_max_freq")

	cfg := DefaultCronosConfig()
	cfg.ThermalRoot = filepath.Join(tmp, "sys/class/thermal")
	// Primary path does not exist
	cfg.CpufreqLimitPath = filepath.Join(tmp, "nonexistent/proc/cpufreq_limited")
	cfg.FallbackScalingMaxPath = fallbackPath

	g := New(cfg)
	snap, err := g.Step()
	if err != nil {
		t.Fatalf("Step() error: %v", err)
	}
	if snap.Degraded {
		t.Fatalf("unexpected degraded state")
	}

	content, err := os.ReadFile(fallbackPath)
	if err != nil {
		t.Fatalf("read fallback file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "1300000" {
		t.Errorf("fallback content = %q, want 1300000", strings.TrimSpace(string(content)))
	}
}
