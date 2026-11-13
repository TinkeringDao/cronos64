// Package robot implements sensor-fused gaze tracking and actuation primitives for stationary robot companions.
package robot

import (
	"math"
	"sync"
	"time"
)

const (
	gazeBGAlpha        = 0.08
	gazeDeltaThreshold = 10.0
	gazeDecayWindow    = 1800 * time.Millisecond
	gazeMinPersonScale = 0.35
)

// GazeSample is one normalized gaze target on [-1.0, +1.0], where +X is right (or viewer-right when
// MirrorX is set) and +Y is down, with Confidence in [0.0, 1.0].
type GazeSample struct {
	X          float64
	Y          float64
	Confidence float64
	At         time.Time
}

// GazeTracker computes a 30 Hz luma motion centroid over small grids (e.g. 64x48), combining a slow
// background EMA with frame-to-frame deltas and a gentle upper-central human face-band prior.
type GazeTracker struct {
	// MirrorX negates horizontal coordinates so an unmirrored front-facing camera sensor (where viewer-left
	// lands on sensor right, x > w/2) maps to X < 0 (screen-left), matching mic.Azimuth().
	MirrorX bool

	mu         sync.Mutex
	w, h       int
	bg         []float64
	prev       []uint8
	lastX      float64
	lastY      float64
	lastConf   float64
	peakConf   float64
	lastMoveAt time.Time
	hasTarget  bool
}

// NewGazeTracker returns a ready-to-use GazeTracker. A zero-value GazeTracker is also valid.
func NewGazeTracker() *GazeTracker {
	return &GazeTracker{}
}

// ProcessLuma updates the tracker with a w-by-h luma grid, scaling fresh motion confidence by
// max(0.35, personScore) and decaying smoothly to zero over 1.8 s when motion stops.
func (t *GazeTracker) ProcessLuma(luma []uint8, w, h int, personScore float64, at time.Time) GazeSample {
	if len(luma) == 0 || w <= 0 || h <= 0 || len(luma) < w*h {
		return GazeSample{X: 0, Y: 0, Confidence: 0, At: at}
	}
	if math.IsNaN(personScore) || math.IsInf(personScore, 0) || personScore < 0 {
		personScore = 0
	} else if personScore > 1 {
		personScore = 1
	}

	n := w * h

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.w != w || t.h != h || len(t.prev) != n || len(t.bg) != n {
		t.w, t.h = w, h
		t.prev = make([]uint8, n)
		t.bg = make([]float64, n)
		for i := range n {
			v := luma[i]
			t.prev[i] = v
			t.bg[i] = float64(v)
		}
		t.lastX, t.lastY = 0, 0
		t.lastConf, t.peakConf = 0, 0
		t.lastMoveAt = time.Time{}
		t.hasTarget = false
		return GazeSample{X: 0, Y: 0, Confidence: 0, At: at}
	}

	var curSum int
	for i := range n {
		curSum += int(luma[i])
	}
	meanCur := float64(curSum) / float64(n)

	var (
		activeCells int
		deltaSum    float64
		sumW        float64
		sumWX       float64
		sumWY       float64
	)

	cx := float64(w-1) / 2.0
	cy := float64(h-1) / 2.0

	for y := range h {
		var ny float64
		if h > 1 {
			ny = (float64(y) - cy) / cy
		}
		row := y * w
		for x := range w {
			idx := row + x
			cur := float64(luma[idx])
			prev := float64(t.prev[idx])
			bg := t.bg[idx]

			dPrev := math.Abs(cur - prev)
			dBg := math.Abs(cur - bg)

			t.bg[idx] = bg + gazeBGAlpha*(cur-bg)
			t.prev[idx] = luma[idx]

			if dPrev < gazeDeltaThreshold {
				continue
			}

			var nx float64
			if w > 1 {
				nx = (float64(x) - cx) / cx
			}

			// Gentle upper-central prior (human face band around nx=0, ny=-0.25).
			prior := 1.0 + 0.35*math.Exp(-(nx*nx)/(2*0.65*0.65)-((ny+0.25)*(ny+0.25))/(2*0.55*0.55))

			// Weight cells by both temporal/background change and foreground contrast from frame mean so
			// a subject moving from A to B tracks the arrived patch B rather than the vacated ghost A.
			dContrast := math.Abs(cur - meanCur)
			fgWeight := (0.5*dPrev + 0.5*dBg) * (0.15 + dContrast/128.0) * prior

			activeCells++
			deltaSum += dPrev
			sumW += fgWeight
			sumWX += fgWeight * nx
			sumWY += fgWeight * ny
		}
	}

	minCells := 1
	if n >= 64 {
		minCells = max(4, n/750)
	}

	if activeCells >= minCells && sumW > 0 {
		rawX := clampUnit(sumWX / sumW)
		rawY := clampUnit(sumWY / sumW)
		if t.MirrorX && rawX != 0 {
			rawX = -rawX
		}

		if !t.hasTarget || t.lastConf == 0 {
			t.lastX, t.lastY = rawX, rawY
			t.hasTarget = true
		} else {
			t.lastX = clampUnit(t.lastX + 0.85*(rawX-t.lastX))
			t.lastY = clampUnit(t.lastY + 0.85*(rawY-t.lastY))
		}

		coverage := float64(activeCells) / float64(n)
		covFactor := min(1.0, 0.40+0.60*(coverage/0.015))
		meanDelta := deltaSum / float64(activeCells)
		ampFactor := min(1.0, meanDelta/25.0)
		scale := max(gazeMinPersonScale, personScore)
		conf := clampZeroOne(covFactor * ampFactor * scale)

		t.peakConf = conf
		t.lastConf = conf
		t.lastMoveAt = at

		return GazeSample{
			X:          sanitizeCoord(t.lastX),
			Y:          sanitizeCoord(t.lastY),
			Confidence: conf,
			At:         at,
		}
	}

	if !t.hasTarget || t.lastMoveAt.IsZero() || t.peakConf <= 0 {
		t.lastConf = 0
		return GazeSample{X: 0, Y: 0, Confidence: 0, At: at}
	}

	dt := at.Sub(t.lastMoveAt)
	if dt >= gazeDecayWindow {
		t.lastConf = 0
		t.peakConf = 0
		return GazeSample{
			X:          sanitizeCoord(t.lastX),
			Y:          sanitizeCoord(t.lastY),
			Confidence: 0,
			At:         at,
		}
	}

	var conf float64
	if dt > 0 {
		conf = t.peakConf * (1.0 - float64(dt)/float64(gazeDecayWindow))
	} else {
		conf = t.lastConf * 0.9
	}
	conf = clampZeroOne(conf)
	t.lastConf = conf

	return GazeSample{
		X:          sanitizeCoord(t.lastX),
		Y:          sanitizeCoord(t.lastY),
		Confidence: conf,
		At:         at,
	}
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampZeroOne(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func sanitizeCoord(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v == 0 {
		return 0
	}
	return v
}
