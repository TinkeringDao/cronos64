// Package robot hosts the Cronos stationary companion brain: fusing voice turn
// phase, TDOA speech azimuth, demand-gated camera gaze, touch reactions, and
// thermal degradation state through a critically damped 2D attention spring.
package robot

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/feature/mute"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/thermal"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/actuate"
	esphome "github.com/ygelfand/go-esphome-device"
)

const (
	// Gaze camera sample resolution (12:9 downscaled luma).
	gazeFrameWidth  = 64
	gazeFrameHeight = 48

	// Critically damped natural frequency in rad/s.
	springOmega = 14.0
	maxSpringDT = 0.1

	// Attention and gesture timing windows.
	viewActiveWindow = 2 * time.Second
	tdoaFreshWindow  = 800 * time.Millisecond
	tdoaMinConf      = 0.45
	gazeFreshWindow  = 1800 * time.Millisecond
	gazeMinConf      = 0.35

	petDuration      = 2 * time.Second
	doubleTapWindow  = 450 * time.Millisecond
	startledDuration = 600 * time.Millisecond
	curiousDuration  = 1500 * time.Millisecond

	thermalInterval = 2 * time.Second
	activePoll      = 100 * time.Millisecond
)

// Mood enumerates the companion facial and behavioral expression states.
type Mood string

const (
	MoodCalm      Mood = "calm"
	MoodCurious   Mood = "curious"
	MoodHappy     Mood = "happy"
	MoodListening Mood = "listening"
	MoodThinking  Mood = "thinking"
	MoodSpeaking  Mood = "speaking"
	MoodSleepy    Mood = "sleepy"
	MoodStartled  Mood = "startled"
)

// String implements fmt.Stringer.
func (m Mood) String() string { return string(m) }

// Snapshot captures the instantaneous companion state rendered at ~60 Hz.
type Snapshot struct {
	LookX           float64
	LookY           float64
	Mood            Mood
	MouthOpen       float64
	ListenPulse     float64
	PetGlow         float64
	Phase           string
	ThermalDegraded bool
}

// Robot fuses companion sensors, touch interactions, thermal state, and the
// guarded Reachy actuation seam.
type Robot struct {
	mu     sync.Mutex
	gazeMu sync.Mutex
	camMu  sync.Mutex

	gaze         *GazeTracker
	thermalLimit *esphome.Sensor
	actuate      *actuate.GuardedSender
	poseCh       chan actuate.Pose
	wake         chan struct{}

	// Injectable hardware & clock hooks (pure at construction; overridden in tests).
	now          func() time.Time
	cronos       func() bool
	camAvailable func() bool
	camAcquire   func() (func(), error)
	camListen    func(func([]uint8, int, int)) func()
	micAzimuth   func() (float64, float64, bool)
	isMuted      func() bool
	isCovered    func() bool
	thermalGuard *thermal.Guard

	// Protected by camMu.
	camRelease  func()
	camUnlisten func()
	camHeld     bool

	// Protected by mu.
	running         bool
	asleep          bool
	lastViewedAt    time.Time
	phase           string
	speechLevel     float64
	listenLevel     float64
	turnAt          time.Time
	az              float64
	azConf          float64
	azOK            bool
	azAt            time.Time
	lastGaze        GazeSample
	lookX           float64
	lookY           float64
	velX            float64
	velY            float64
	lastSpringAt    time.Time
	epochAt         time.Time
	petAt           time.Time
	petUntil        time.Time
	startledUntil   time.Time
	curiousUntil    time.Time
	lastTapAt       time.Time
	lastTapInCenter bool
	dragActive      bool
	dragLastX       int
	dragLastY       int

	thermalDegraded     atomic.Bool
	thermalMu           sync.Mutex
	thermalSeen         bool
	lastThermalKHz      int
	lastThermalDegraded bool
	lastThermalErr      string
}

type testDeps struct {
	now          func() time.Time
	cronos       func() bool
	camAvailable func() bool
	camAcquire   func() (func(), error)
	camListen    func(func([]uint8, int, int)) func()
	micAzimuth   func() (float64, float64, bool)
	isMuted      func() bool
	isCovered    func() bool
	thermalGuard *thermal.Guard
}

var (
	once   sync.Once
	shared *Robot
)

func init() {
	component.Register(component.Device, Get(), component.Order(55))
}

// Get returns the singleton companion controller without touching hardware.
func Get() *Robot {
	once.Do(func() { shared = newRobot(testDeps{}) })
	return shared
}

func newForTest(deps testDeps) *Robot {
	return newRobot(deps)
}

func newRobot(deps testDeps) *Robot {
	nowFn := deps.now
	if nowFn == nil {
		nowFn = time.Now
	}
	cronosFn := deps.cronos
	if cronosFn == nil {
		cronosFn = layout.Cronos
	}
	camAvailFn := deps.camAvailable
	if camAvailFn == nil {
		camAvailFn = camera.Available
	}
	camAcqFn := deps.camAcquire
	if camAcqFn == nil {
		camAcqFn = func() (func(), error) { return camera.Get().Acquire() }
	}
	camListenFn := deps.camListen
	if camListenFn == nil {
		camListenFn = func(cb func([]uint8, int, int)) func() {
			return camera.Get().Frames.Listen(func(fr *camera.Frame) {
				if fr == nil {
					return
				}
				cb(fr.Luma(gazeFrameWidth, gazeFrameHeight), gazeFrameWidth, gazeFrameHeight)
			})
		}
	}
	micAzFn := deps.micAzimuth
	if micAzFn == nil {
		micAzFn = func() (float64, float64, bool) { return mic.Get().Azimuth() }
	}
	mutedFn := deps.isMuted
	if mutedFn == nil {
		mutedFn = func() bool {
			m, err := mute.Get().Muted()
			return err == nil && m
		}
	}
	coveredFn := deps.isCovered
	if coveredFn == nil {
		coveredFn = lensCovered
	}

	return &Robot{
		// Initialized upfront so concurrent camera frame callbacks never lazily allocate it.
		gaze: &GazeTracker{MirrorX: true},
		thermalLimit: &esphome.Sensor{
			Base: esphome.Base{
				ObjectID: "cpu_thermal_limit",
				Name:     "CPU Thermal Limit",
				Icon:     "mdi:speedometer",
				Category: esphome.CategoryDiagnostic,
			},
			Unit:       "kHz",
			StateClass: esphome.StateClassMeasurement,
			Decimals:   0,
		},
		actuate:      actuate.NewGuardedSender(actuate.NoopTransport{}),
		poseCh:       make(chan actuate.Pose, 1),
		wake:         make(chan struct{}, 1),
		now:          nowFn,
		cronos:       cronosFn,
		camAvailable: camAvailFn,
		camAcquire:   camAcqFn,
		camListen:    camListenFn,
		micAzimuth:   micAzFn,
		isMuted:      mutedFn,
		isCovered:    coveredFn,
		thermalGuard: deps.thermalGuard,
		phase:        "idle",
		epochAt:      nowFn(),
	}
}

// Name implements component.Component.
func (r *Robot) Name() string { return "robot" }

// Entities implements component.Entities.
func (r *Robot) Entities() []esphome.Entity {
	return []esphome.Entity{r.thermalLimit}
}

// SetTransport sets the downstream actuation transport used by the guarded seam.
func (r *Robot) SetTransport(t actuate.Transport) {
	prev := r.actuate.SetTransport(t)
	if prev != nil {
		_ = prev.Close()
	}
}

// SetAsleep notifies the companion that the panel is dark/sleeping so camera demand releases immediately.
func (r *Robot) SetAsleep(asleep bool) {
	r.mu.Lock()
	changed := r.asleep != asleep
	r.asleep = asleep
	r.mu.Unlock()
	if changed {
		r.poke()
	}
}

// ObserveTurn feeds the current voice phase and normalized speaker/mic levels [0, 1].
func (r *Robot) ObserveTurn(phase string, speechLevel, listenLevel float64, now time.Time) {
	if phase == "" {
		phase = "idle"
	}
	r.mu.Lock()
	phaseChanged := r.phase != phase
	r.phase = phase
	r.speechLevel = clamp01(speechLevel)
	r.listenLevel = clamp01(listenLevel)
	if phaseChanged || r.turnAt.IsZero() {
		r.turnAt = now
	}
	if phaseChanged && phase != "listening" {
		r.azOK = false
	}
	r.mu.Unlock()
	if phaseChanged {
		if phase == "listening" {
			// Arm demand for a fresh stereo frame instead of reading a stale pre-turn sample.
			_, _, _ = r.micAzimuth()
		}
		r.poke()
	}
}

func (r *Robot) observePhase(phase string, now time.Time) {
	if phase == "" {
		phase = "idle"
	}
	r.mu.Lock()
	phaseChanged := r.phase != phase
	r.phase = phase
	if phaseChanged || r.turnAt.IsZero() {
		r.turnAt = now
	}
	if phaseChanged {
		if phase != "replying" && phase != "lingering" {
			r.speechLevel = 0
		}
		if phase != "listening" {
			r.listenLevel = 0
			r.azOK = false
		}
	}
	r.mu.Unlock()
	if phaseChanged {
		if phase == "listening" {
			_, _, _ = r.micAzimuth()
		}
		r.poke()
	}
}

// ObserveAzimuth injects a fresh TDOA azimuth reading for fusion during listening turns.
func (r *Robot) ObserveAzimuth(azimuth, confidence float64, ok bool, now time.Time) {
	if !ok || math.IsNaN(azimuth) || math.IsInf(azimuth, 0) || math.IsNaN(confidence) || math.IsInf(confidence, 0) {
		return
	}
	r.mu.Lock()
	r.az = clamp(azimuth, -1, 1)
	r.azConf = clamp01(confidence)
	r.azOK = true
	if r.azConf >= tdoaMinConf {
		r.azAt = now
	}
	r.mu.Unlock()
}

// ObserveGaze records a gaze tracker sample directly (or from ProcessLuma).
func (r *Robot) ObserveGaze(sample GazeSample) {
	r.mu.Lock()
	if sample.Confidence >= gazeMinConf && !sample.At.IsZero() {
		r.lastGaze = sample
	} else if r.lastGaze.At.IsZero() || sample.At.After(r.lastGaze.At) {
		r.lastGaze = sample
	}
	r.mu.Unlock()
}

// Snapshot steps the critically damped attention spring to now, resolves mood/pulses,
// marks the companion face as actively viewed, and emits a bounded pose if moving.
func (r *Robot) Snapshot(now time.Time) Snapshot {
	r.mu.Lock()
	wasStale := r.lastViewedAt.IsZero() || now.Sub(r.lastViewedAt) > viewActiveWindow
	r.lastViewedAt = now

	if r.lastSpringAt.IsZero() {
		r.lastSpringAt = now
	}
	dt := now.Sub(r.lastSpringAt).Seconds()
	if dt < 0 {
		dt = 0
	} else if dt > maxSpringDT {
		dt = maxSpringDT
	}
	r.lastSpringAt = now

	tx, ty := r.targetLocked(now)
	if dt > 0 {
		r.lookX, r.velX = stepCriticallyDamped(r.lookX, r.velX, tx, springOmega, dt)
		r.lookY, r.velY = stepCriticallyDamped(r.lookY, r.velY, ty, springOmega, dt)
		r.lookX = clamp(r.lookX, -1, 1)
		r.lookY = clamp(r.lookY, -1, 1)
	}

	mood := r.moodLocked(now)
	mouthOpen := r.mouthOpenLocked(now, mood)
	listenPulse := r.listenPulseLocked(now, mood)
	petGlow := r.petGlowLocked(now)
	phase := r.phase
	lookX, lookY := r.lookX, r.lookY
	running := r.running
	r.mu.Unlock()

	if wasStale {
		r.poke()
	}

	snap := Snapshot{
		LookX:           lookX,
		LookY:           lookY,
		Mood:            mood,
		MouthOpen:       mouthOpen,
		ListenPulse:     listenPulse,
		PetGlow:         petGlow,
		Phase:           phase,
		ThermalDegraded: r.thermalDegraded.Load(),
	}
	r.emitPose(snap, running)
	return snap
}

// Run supervises thermal throttling, demand-gated camera capture, voice/mute/cover
// hooks, and async pose actuation until ctx is canceled.
func (r *Robot) Run(ctx context.Context) error {
	if r.cronos() && r.thermalGuard == nil {
		r.thermalGuard = thermal.New(thermal.DefaultCronosConfig())
	}

	r.mu.Lock()
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		r.releaseCamera()
		_ = r.actuate.Close()
	}()

	unVoice := voice.Changed.Listen(func(st voice.State) {
		r.observePhase(st.Phase, r.now())
	})
	defer unVoice()

	unMute := mute.Get().Changed.Listen(func(bool) { r.poke() })
	defer unMute()

	unCover := listenLensCover(func() { r.poke() })
	defer unCover()

	// Forward queued pose frames without stalling the 60 Hz display goroutine.
	poseDone := make(chan struct{})
	go func() {
		defer close(poseDone)
		for {
			select {
			case <-ctx.Done():
				return
			case p := <-r.poseCh:
				_ = r.actuate.Send(ctx, p)
			}
		}
	}()
	defer func() { <-poseDone }()

	now := r.now()
	r.stepThermal(now)
	r.stepSupervision(now)

	thermalTicker := time.NewTicker(thermalInterval)
	defer thermalTicker.Stop()

	sensorTicker := time.NewTicker(activePoll)
	defer sensorTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.wake:
			r.stepSupervision(r.now())
		case <-thermalTicker.C:
			r.stepThermal(r.now())
		case <-sensorTicker.C:
			r.stepSupervision(r.now())
		}
	}
}

func (r *Robot) stepThermal(now time.Time) {
	_ = now
	if !r.cronos() || r.thermalGuard == nil {
		return
	}
	snap, err := r.thermalGuard.Step()
	nominal := thermal.DefaultCronosConfig().OPPs[0]
	degraded := snap.Degraded || (snap.LimitKHz > 0 && snap.LimitKHz < nominal)

	hasLimitOrDegraded := snap.LimitKHz > 0 || snap.Degraded
	if hasLimitOrDegraded {
		r.thermalDegraded.Store(degraded)
		if snap.LimitKHz > 0 {
			r.thermalLimit.Set(float32(snap.LimitKHz))
		}
	}

	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	r.thermalMu.Lock()
	stateChanged := hasLimitOrDegraded &&
		(!r.thermalSeen || snap.LimitKHz != r.lastThermalKHz || degraded != r.lastThermalDegraded)
	if hasLimitOrDegraded {
		r.thermalSeen = true
		r.lastThermalKHz = snap.LimitKHz
		r.lastThermalDegraded = degraded
	}
	errChanged := errStr != r.lastThermalErr
	r.lastThermalErr = errStr
	r.thermalMu.Unlock()

	if err != nil && errChanged {
		slog.Warn("robot: thermal guard step failed", "err", err, "limit_khz", snap.LimitKHz, "degraded", degraded)
	}
	if stateChanged {
		if degraded {
			slog.Warn("robot: thermal OPP degraded", "limit_khz", snap.LimitKHz, "temp_mc", snap.TempMilliC, "degraded", degraded)
		} else {
			slog.Info("robot: thermal OPP updated", "limit_khz", snap.LimitKHz, "temp_mc", snap.TempMilliC, "degraded", degraded)
		}
	}
}

func (r *Robot) stepSupervision(now time.Time) {
	r.mu.Lock()
	viewed := !r.lastViewedAt.IsZero() && now.Sub(r.lastViewedAt) <= viewActiveWindow
	asleep := r.asleep
	listening := r.phase == "listening"
	r.mu.Unlock()

	if listening {
		if az, conf, ok := r.micAzimuth(); ok {
			r.ObserveAzimuth(az, conf, true, now)
		}
	}

	wantCam := viewed && !asleep && !r.isMuted() && !r.isCovered() && r.camAvailable()
	if wantCam {
		r.acquireCamera()
	} else {
		r.releaseCamera()
	}
}

func (r *Robot) acquireCamera() {
	r.camMu.Lock()
	defer r.camMu.Unlock()
	if r.camHeld {
		return
	}
	rel, err := r.camAcquire()
	if err != nil || rel == nil {
		return
	}
	unsub := r.camListen(func(luma []uint8, w, h int) {
		if len(luma) == 0 || w <= 0 || h <= 0 {
			return
		}
		now := r.now()
		r.gazeMu.Lock()
		sample := r.gaze.ProcessLuma(luma, w, h, 0, now)
		r.gazeMu.Unlock()
		if sample.Confidence > 0 {
			r.ObserveGaze(sample)
		}
	})
	r.camRelease = rel
	r.camUnlisten = unsub
	r.camHeld = true
}

func (r *Robot) releaseCamera() {
	r.camMu.Lock()
	if !r.camHeld {
		r.camMu.Unlock()
		return
	}
	unsub := r.camUnlisten
	rel := r.camRelease
	r.camUnlisten = nil
	r.camRelease = nil
	r.camHeld = false
	r.camMu.Unlock()

	if unsub != nil {
		unsub()
	}
	if rel != nil {
		rel()
	}
}

func (r *Robot) targetLocked(now time.Time) (float64, float64) {
	if r.asleep {
		return 0, 0.25
	}
	// (a) Active listening + fresh TDOA azimuth.
	if r.phase == "listening" && r.azOK && r.azConf >= tdoaMinConf && !r.azAt.IsZero() && now.Sub(r.azAt) <= tdoaFreshWindow {
		return clamp(r.az, -1, 1), -0.04
	}
	// (b) Fresh camera gaze estimate.
	if !r.lastGaze.At.IsZero() && r.lastGaze.Confidence >= gazeMinConf && now.Sub(r.lastGaze.At) <= gazeFreshWindow {
		return clamp(r.lastGaze.X, -1, 1), clamp(r.lastGaze.Y, -1, 1)
	}
	// (c) Calm ambient wander.
	sec := now.Sub(r.epochAt).Seconds()
	tx := 0.22 * math.Sin(sec*0.43)
	ty := -0.04 + 0.08*math.Sin(sec*0.31+0.9)
	return tx, ty
}

func (r *Robot) moodLocked(now time.Time) Mood {
	if now.Before(r.startledUntil) {
		return MoodStartled
	}
	if r.asleep {
		return MoodSleepy
	}
	switch r.phase {
	case "listening":
		return MoodListening
	case "thinking":
		return MoodThinking
	case "replying":
		return MoodSpeaking
	}
	if now.Before(r.petUntil) {
		return MoodHappy
	}
	if now.Before(r.curiousUntil) {
		return MoodCurious
	}
	if r.phase == "lingering" && r.speechLevel > 0.05 {
		return MoodSpeaking
	}
	return MoodCalm
}

func (r *Robot) mouthOpenLocked(now time.Time, mood Mood) float64 {
	if mood != MoodSpeaking {
		return 0
	}
	if r.speechLevel > 0 {
		return clamp01(r.speechLevel)
	}
	// Gentle fallback modulation if phase is replying before live speaker samples arrive.
	sec := now.Sub(r.turnAt).Seconds()
	return clamp01(0.28 + 0.24*math.Sin(sec*9.0))
}

func (r *Robot) listenPulseLocked(now time.Time, mood Mood) float64 {
	if mood != MoodListening {
		return 0
	}
	sec := now.Sub(r.turnAt).Seconds()
	base := 0.35 + 0.25*(0.5+0.5*math.Sin(sec*4.2))
	return clamp01(math.Max(r.listenLevel, base))
}

func (r *Robot) petGlowLocked(now time.Time) float64 {
	if r.petUntil.IsZero() || !now.Before(r.petUntil) {
		return 0
	}
	rem := r.petUntil.Sub(now).Seconds()
	return clamp01(rem / petDuration.Seconds())
}

func (r *Robot) emitPose(snap Snapshot, async bool) {
	roll := 0.0
	switch snap.Mood {
	case MoodCurious:
		roll = 8
	case MoodHappy:
		roll = 4 * snap.PetGlow
	}
	pose := actuate.Pose{
		Yaw:      snap.LookX * 35.0,
		Pitch:    -snap.LookY * 18.0,
		Roll:     roll,
		Mood:     string(snap.Mood),
		Speaking: snap.MouthOpen,
		TTLMS:    actuate.DefaultTTLMS,
	}
	if !async {
		_ = r.actuate.Send(context.Background(), pose)
		return
	}
	select {
	case r.poseCh <- pose:
	default:
		// Replace stale unconsumed pose with newest frame without blocking display.
		select {
		case <-r.poseCh:
		default:
		}
		select {
		case r.poseCh <- pose:
		default:
		}
	}
}

func (r *Robot) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// stepCriticallyDamped advances position x and velocity v toward target over dt > 0
// with exact analytical critically damped dynamics (zeta = 1), preventing overshoot.
func stepCriticallyDamped(x, v, target, omega, dt float64) (float64, float64) {
	e0 := x - target
	decay := math.Exp(-omega * dt)
	c := v + omega*e0
	nx := target + (e0+c*dt)*decay
	nv := (v - omega*c*dt) * decay
	return nx, nv
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }
