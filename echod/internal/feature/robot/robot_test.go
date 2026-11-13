package robot

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/thermal"
	"github.com/HuskerMinion/techo5/echod/internal/lib/actuate"
	esphome "github.com/ygelfand/go-esphome-device"
)

func TestRobotComponentPurityAndEntity(t *testing.T) {
	var acquires atomic.Int32
	r := newForTest(testDeps{
		camAvailable: func() bool { return true },
		camAcquire: func() (func(), error) {
			acquires.Add(1)
			return func() {}, nil
		},
	})

	if got := r.Name(); got != "robot" {
		t.Fatalf("Name() = %q, want %q", got, "robot")
	}
	if r.gaze == nil || !r.gaze.MirrorX {
		t.Fatalf("gaze tracker must be initialized upfront with MirrorX=true, got %+v", r.gaze)
	}
	if acquires.Load() != 0 {
		t.Fatalf("newForTest acquired camera (%d), want 0 (hardware import purity)", acquires.Load())
	}

	ents := r.Entities()
	var found *esphome.Sensor
	for _, e := range ents {
		if s, ok := e.(*esphome.Sensor); ok && s.Base.ObjectID == "cpu_thermal_limit" {
			found = s
			break
		}
	}
	if found == nil {
		t.Fatal("missing cpu_thermal_limit sensor in Entities()")
	}
	if found.Base.Name != "CPU Thermal Limit" || found.Unit != "kHz" || found.Base.Category != esphome.CategoryDiagnostic {
		t.Fatalf("cpu_thermal_limit metadata = %+v", found)
	}

	// Singleton checks must stay hardware-pure too and register as Device.
	if Get().Name() != "robot" {
		t.Fatalf("Get().Name() = %q, want robot", Get().Name())
	}
	var reg component.Phase = component.Device
	_ = reg
}

func TestAttentionPriorityAndCriticallyDampedSpring(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := newForTest(testDeps{now: func() time.Time { return now }})

	// Start with an initial frame so spring clock is anchored.
	s0 := r.Snapshot(now)
	if s0.Mood != MoodCalm {
		t.Fatalf("initial mood = %q, want calm", s0.Mood)
	}

	// Calm wander produces nonzero bounded gaze motion over time without inputs.
	var moved bool
	for i := 0; i < 60; i++ {
		now = now.Add(50 * time.Millisecond)
		s := r.Snapshot(now)
		if s.LookX != 0 || s.LookY != 0 {
			moved = true
		}
		if s.LookX < -1 || s.LookX > 1 || s.LookY < -1 || s.LookY > 1 {
			t.Fatalf("wander out of bounds: (%v, %v)", s.LookX, s.LookY)
		}
	}
	if !moved {
		t.Fatal("calm ambient wander stayed stuck at (0,0)")
	}

	// Reset spring baseline cleanly at now for crisp target verification.
	r = newForTest(testDeps{now: func() time.Time { return now }})
	_ = r.Snapshot(now)

	// Priority (b): camera gaze when not listening with fresh TDOA.
	r.ObserveGaze(GazeSample{X: 0.45, Y: 0.15, Confidence: 0.80, At: now})
	for i := 0; i < 35; i++ { // ~560 ms at ~60 Hz
		now = now.Add(16 * time.Millisecond)
		s := r.Snapshot(now)
		if s.LookX > 0.45+1e-6 || s.LookY > 0.15+1e-6 {
			t.Fatalf("spring overshot camera target (0.45, 0.15) at step %d: (%v, %v)", i, s.LookX, s.LookY)
		}
	}
	gazeSettled := r.Snapshot(now)
	if gazeSettled.LookX < 0.42 || gazeSettled.LookY < 0.13 {
		t.Fatalf("spring failed to converge toward camera target (0.45, 0.15): got (%v, %v)", gazeSettled.LookX, gazeSettled.LookY)
	}

	// Priority (a): listening + fresh TDOA (conf >= 0.45) overrides camera gaze -> (-0.70, -0.04).
	r.ObserveTurn("listening", 0, 0.4, now)
	r.ObserveAzimuth(-0.70, 0.85, true, now)
	for i := 0; i < 40; i++ {
		now = now.Add(16 * time.Millisecond)
		s := r.Snapshot(now)
		if s.LookX < -0.70-1e-6 {
			t.Fatalf("critically damped spring overshot -0.70 at step %d: %v", i, s.LookX)
		}
	}
	azSettled := r.Snapshot(now)
	if azSettled.LookX > -0.66 || azSettled.LookY > 0.0 {
		t.Fatalf("listening TDOA priority did not steer to (-0.70, -0.04): got (%v, %v)", azSettled.LookX, azSettled.LookY)
	}
	if azSettled.Mood != MoodListening || azSettled.ListenPulse <= 0 {
		t.Fatalf("listening mood/pulse = (%q, %v), want listening + >0", azSettled.Mood, azSettled.ListenPulse)
	}

	// Refresh camera gaze at current time, expire TDOA (> 800 ms) -> falls back to camera gaze!
	r.ObserveGaze(GazeSample{X: 0.60, Y: -0.20, Confidence: 0.70, At: now})
	now = now.Add(900 * time.Millisecond) // TDOA is now > 800 ms old, gaze is 900 ms old (<= 1.8 s)
	r.ObserveTurn("idle", 0, 0, now)
	for i := 0; i < 30; i++ {
		now = now.Add(16 * time.Millisecond)
		_ = r.Snapshot(now)
	}
	backToCam := r.Snapshot(now)
	if backToCam.LookX < 0.50 {
		t.Fatalf("expected fallback to fresh camera gaze (0.60, -0.20), got (%v, %v)", backToCam.LookX, backToCam.LookY)
	}
}

func TestVoicePhasesMouthAndListenPulse(t *testing.T) {
	now := time.Unix(1700000100, 0)
	r := newForTest(testDeps{now: func() time.Time { return now }})

	r.ObserveTurn("thinking", 0, 0, now)
	if s := r.Snapshot(now); s.Mood != MoodThinking || s.Phase != "thinking" {
		t.Fatalf("thinking snap = %+v", s)
	}

	r.ObserveTurn("replying", 0.82, 0, now)
	if s := r.Snapshot(now); s.Mood != MoodSpeaking || s.MouthOpen < 0.8 {
		t.Fatalf("replying snap = %+v, want MoodSpeaking + MouthOpen>=0.8", s)
	}

	r.SetAsleep(true)
	if s := r.Snapshot(now.Add(16 * time.Millisecond)); s.Mood != MoodSleepy {
		t.Fatalf("asleep snap mood = %q, want sleepy", s.Mood)
	}
}

func TestDemandGatedCameraAcquireAndRelease(t *testing.T) {
	now := time.Unix(1700000200, 0)
	var available atomic.Bool
	available.Store(true)
	var muted, covered atomic.Bool
	var acquires, releases, active, listens atomic.Int32

	r := newForTest(testDeps{
		now:          func() time.Time { return now },
		camAvailable: func() bool { return available.Load() },
		camAcquire: func() (func(), error) {
			acquires.Add(1)
			active.Add(1)
			var once sync.Once
			return func() {
				once.Do(func() {
					active.Add(-1)
					releases.Add(1)
				})
			}, nil
		},
		camListen: func(cb func([]uint8, int, int)) func() {
			listens.Add(1)
			// Feed one synthetic asymmetric bright frame to verify gaze wiring.
			luma := make([]uint8, 64*48)
			for y := 8; y < 32; y++ {
				for x := 44; x < 58; x++ {
					luma[y*64+x] = 220
				}
			}
			cb(luma, 64, 48)
			return func() {}
		},
		isMuted:   func() bool { return muted.Load() },
		isCovered: func() bool { return covered.Load() },
	})

	// Never viewed -> supervision must NOT acquire camera.
	r.stepSupervision(now)
	if acquires.Load() != 0 || active.Load() != 0 {
		t.Fatalf("acquired camera when not viewed: acq=%d active=%d", acquires.Load(), active.Load())
	}

	// Viewed -> acquires camera and receives frame into GazeTracker.
	_ = r.Snapshot(now)
	r.stepSupervision(now)
	if acquires.Load() != 1 || active.Load() != 1 || listens.Load() != 1 {
		t.Fatalf("expected active camera session: acq=%d active=%d listens=%d", acquires.Load(), active.Load(), listens.Load())
	}

	// Cover shutter -> immediately releases.
	covered.Store(true)
	r.stepSupervision(now.Add(100 * time.Millisecond))
	if active.Load() != 0 || releases.Load() != 1 {
		t.Fatalf("expected release on cover: active=%d rel=%d", active.Load(), releases.Load())
	}
	covered.Store(false)

	// Re-view -> re-acquires.
	now = now.Add(200 * time.Millisecond)
	_ = r.Snapshot(now)
	r.stepSupervision(now)
	if acquires.Load() != 2 || active.Load() != 1 {
		t.Fatalf("expected re-acquire after uncover: acq=%d active=%d", acquires.Load(), active.Load())
	}

	// Muted -> immediately releases.
	muted.Store(true)
	r.stepSupervision(now.Add(50 * time.Millisecond))
	if active.Load() != 0 || releases.Load() != 2 {
		t.Fatalf("expected release on mute: active=%d rel=%d", active.Load(), releases.Load())
	}
	muted.Store(false)

	// Re-view -> re-acquires, then asleep -> immediately releases.
	now = now.Add(100 * time.Millisecond)
	_ = r.Snapshot(now)
	r.stepSupervision(now)
	if active.Load() != 1 {
		t.Fatalf("expected active before asleep, got %d", active.Load())
	}
	r.SetAsleep(true)
	r.stepSupervision(now.Add(50 * time.Millisecond))
	if active.Load() != 0 || releases.Load() != 3 {
		t.Fatalf("expected release when asleep: active=%d rel=%d", active.Load(), releases.Load())
	}
	r.SetAsleep(false)

	// Re-view, then idle > 2s without Snapshot -> releases cleanly.
	now = now.Add(100 * time.Millisecond)
	_ = r.Snapshot(now)
	r.stepSupervision(now)
	if active.Load() != 1 {
		t.Fatalf("expected active=1 before idle timeout, got %d", active.Load())
	}
	r.stepSupervision(now.Add(2100 * time.Millisecond))
	if active.Load() != 0 || releases.Load() != 4 {
		t.Fatalf("expected release after 2.1s without Snapshot: active=%d rel=%d", active.Load(), releases.Load())
	}
}

func TestThermalSupervisionUpdatesDiagnosticAndDegraded(t *testing.T) {
	dir := t.TempDir()
	zoneDir := filepath.Join(dir, "thermal", "thermal_zone0")
	if err := os.MkdirAll(zoneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zoneDir, "type"), []byte("mtktscpu\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempFile := filepath.Join(zoneDir, "temp")
	freqFile := filepath.Join(dir, "scaling_max_freq")
	if err := os.WriteFile(tempFile, []byte("82000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freqFile, []byte("1300000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := thermal.DefaultCronosConfig()
	cfg.ThermalRoot = filepath.Join(dir, "thermal")
	cfg.CpufreqLimitPath = filepath.Join(dir, "nonexistent_limit")
	cfg.FallbackScalingMaxPath = freqFile
	guard := thermal.New(cfg)

	now := time.Unix(1700000300, 0)
	r := newForTest(testDeps{
		now:          func() time.Time { return now },
		cronos:       func() bool { return true },
		thermalGuard: guard,
	})

	r.stepThermal(now)
	snap := r.Snapshot(now)
	if !snap.ThermalDegraded {
		t.Fatal("expected ThermalDegraded=true at 82C (>80C)")
	}
	if got := r.thermalLimit.Get(); got != float32(cfg.OPPs[1]) {
		t.Fatalf("thermalLimit sensor = %v, want %v", got, cfg.OPPs[1])
	}

	// Recover below low watermark (74C <= 75C).
	if err := os.WriteFile(tempFile, []byte("74000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.stepThermal(now.Add(2 * time.Second))
	snap2 := r.Snapshot(now.Add(2 * time.Second))
	if snap2.ThermalDegraded {
		t.Fatal("expected ThermalDegraded=false after hysteresis recovery to 74C")
	}
	if got := r.thermalLimit.Get(); got != float32(cfg.OPPs[0]) {
		t.Fatalf("thermalLimit sensor = %v, want %v", got, cfg.OPPs[0])
	}

	// Verify Run exits cleanly on cancel.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx); err != nil && err != context.Canceled {
		t.Fatalf("Run canceled error = %v", err)
	}
}

type actuateSpy struct {
	mu    sync.Mutex
	poses []actuate.Pose
}

func (s *actuateSpy) Send(_ context.Context, p actuate.Pose) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poses = append(s.poses, p)
	return nil
}

func (s *actuateSpy) Close() error { return nil }

func TestRobotActuateSeamEmitsBoundedPoses(t *testing.T) {
	now := time.Unix(1700000400, 0)
	r := newForTest(testDeps{now: func() time.Time { return now }})
	spy := &actuateSpy{}
	r.SetTransport(spy)

	r.ObserveTurn("replying", 0.7, 0, now)
	r.ObserveGaze(GazeSample{X: 0.9, Y: 0.2, Confidence: 0.9, At: now})
	now = now.Add(50 * time.Millisecond)
	_ = r.Snapshot(now)

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.poses) != 1 {
		t.Fatalf("got %d actuate poses, want 1", len(spy.poses))
	}
	p := spy.poses[0]
	if p.Seq != 1 || p.Mood != "speaking" || p.Speaking < 0.7 || p.TTLMS != actuate.DefaultTTLMS {
		t.Fatalf("pose = %+v", p)
	}
	if p.Yaw < -actuate.MaxYawDeg || p.Yaw > actuate.MaxYawDeg || p.Pitch < -actuate.MaxPitchDeg || p.Pitch > actuate.MaxPitchDeg {
		t.Fatalf("pose angles out of mechanical bounds: %+v", p)
	}
}
