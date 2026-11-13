package robot

import (
	"math"
	"testing"
	"time"
)

func fillLumaPatch(w, h int, bg uint8, px0, py0, px1, py1 int, val uint8) []uint8 {
	buf := make([]uint8, w*h)
	for i := range buf {
		buf[i] = bg
	}
	for y := max(0, py0); y < min(h, py1); y++ {
		for x := max(0, px0); x < min(w, px1); x++ {
			buf[y*w+x] = val
		}
	}
	return buf
}

func TestTrackerCentroidFollowsMovingSubject(t *testing.T) {
	const w, h = 64, 48
	now := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)

	// Case 1: patch enters upper-left quadrant (x=12..24, y=8..20) from a quiet room.
	var tr GazeTracker
	frame0 := fillLumaPatch(w, h, 24, 0, 0, 0, 0, 24)
	frame1 := fillLumaPatch(w, h, 24, 12, 8, 24, 20, 210)

	_ = tr.ProcessLuma(frame0, w, h, 0.85, now)
	got := tr.ProcessLuma(frame1, w, h, 0.85, now.Add(33*time.Millisecond))

	if got.Confidence <= 0.5 {
		t.Fatalf("Confidence = %.3f, want > 0.5", got.Confidence)
	}
	if got.X >= -0.25 {
		t.Fatalf("X = %.3f, want < -0.25 for upper-left patch", got.X)
	}
	if got.Y >= -0.15 {
		t.Fatalf("Y = %.3f, want < -0.15 for upper-left patch", got.Y)
	}

	// Case 2: two frames where an existing 12x12 bright patch moves from lower-right into upper-left (x=12..24, y=8..20).
	var trMove GazeTracker
	startRight := fillLumaPatch(w, h, 24, 40, 28, 52, 40, 210)
	movedLeft := fillLumaPatch(w, h, 24, 12, 8, 24, 20, 210)
	_ = trMove.ProcessLuma(startRight, w, h, 0.85, now)
	gotMove := trMove.ProcessLuma(movedLeft, w, h, 0.85, now.Add(33*time.Millisecond))
	if gotMove.Confidence <= 0.5 || gotMove.X >= -0.25 || gotMove.Y >= -0.15 {
		t.Fatalf("moving patch sample = %+v, want Confidence > 0.5, X < -0.25, Y < -0.15", gotMove)
	}
}

func TestTrackerNilAndZeroMotionLuma(t *testing.T) {
	const w, h = 64, 48
	now := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)

	var tr GazeTracker
	sNil := tr.ProcessLuma(nil, w, h, 0.9, now)
	if sNil.Confidence != 0 || math.IsNaN(sNil.X) || math.IsNaN(sNil.Y) || math.IsInf(sNil.X, 0) || math.IsInf(sNil.Y, 0) {
		t.Fatalf("nil luma sample = %+v, want Confidence == 0 and finite X/Y", sNil)
	}

	// Short slice, zero dims, NaN personScore, and 1x1 edge case must all stay finite and safe.
	for _, bad := range []GazeSample{
		tr.ProcessLuma(make([]uint8, 10), w, h, 0.9, now),
		tr.ProcessLuma(make([]uint8, w*h), 0, h, 0.9, now),
		tr.ProcessLuma(make([]uint8, 1), 1, 1, math.NaN(), now),
	} {
		if bad.Confidence != 0 || math.IsNaN(bad.X) || math.IsNaN(bad.Y) {
			t.Fatalf("bad input sample = %+v, want Confidence == 0 and non-NaN X/Y", bad)
		}
	}

	// Acquire motion first, then feed static identical frames and assert smooth decay to 0 over 1.8 s.
	var trDecay GazeTracker
	bg := fillLumaPatch(w, h, 30, 0, 0, 0, 0, 30)
	subject := fillLumaPatch(w, h, 30, 12, 8, 24, 20, 200)

	_ = trDecay.ProcessLuma(bg, w, h, 0.85, now)
	active := trDecay.ProcessLuma(subject, w, h, 0.85, now.Add(33*time.Millisecond))
	if active.Confidence <= 0.5 {
		t.Fatalf("initial active Confidence = %.3f, want > 0.5", active.Confidence)
	}

	// Brief pause (200 ms): person stopped moving momentarily, eyes should hold gaze with high confidence.
	brief := trDecay.ProcessLuma(subject, w, h, 0.85, now.Add(233*time.Millisecond))
	if brief.Confidence <= 0.4 || brief.Confidence >= active.Confidence {
		t.Fatalf("brief pause Confidence = %.3f, want smooth partial decay below %.3f", brief.Confidence, active.Confidence)
	}
	if math.IsNaN(brief.X) || math.IsNaN(brief.Y) || brief.X >= -0.25 {
		t.Fatalf("brief pause gaze = (%.3f, %.3f), want held upper-left position", brief.X, brief.Y)
	}

	// Mid decay (~1.0 s after motion): lower than brief pause, still > 0.
	mid := trDecay.ProcessLuma(subject, w, h, 0.85, now.Add(1033*time.Millisecond))
	if mid.Confidence <= 0 || mid.Confidence >= brief.Confidence {
		t.Fatalf("mid decay Confidence = %.3f, want 0 < mid < %.3f", mid.Confidence, brief.Confidence)
	}

	// Past 1.8 s of static identical frames: Confidence decays all the way to 0.
	settled := trDecay.ProcessLuma(subject, w, h, 0.85, now.Add(1900*time.Millisecond))
	if settled.Confidence != 0 || math.IsNaN(settled.X) || math.IsNaN(settled.Y) {
		t.Fatalf("settled sample after >1.8s static frames = %+v, want Confidence == 0 and non-NaN X/Y", settled)
	}
}

func TestTrackerMirrorXMapsUnmirroredSensorViewerLeftToNegativeX(t *testing.T) {
	const w, h = 64, 48
	now := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)

	tr := GazeTracker{MirrorX: true}
	bg := fillLumaPatch(w, h, 24, 0, 0, 0, 0, 24)
	// On an unmirrored front-facing OV02B10 sensor, viewer-left lands on sensor right (x > w/2).
	sensorRight := fillLumaPatch(w, h, 24, 40, 8, 52, 20, 210)

	_ = tr.ProcessLuma(bg, w, h, 0.85, now)
	got := tr.ProcessLuma(sensorRight, w, h, 0.85, now.Add(33*time.Millisecond))
	if got.Confidence <= 0.5 {
		t.Fatalf("MirrorX Confidence = %.3f, want > 0.5", got.Confidence)
	}
	if got.X >= -0.25 {
		t.Fatalf("MirrorX X = %.3f, want < -0.25 (screen-left) for camera x=40..52", got.X)
	}
	if got.Y >= -0.15 {
		t.Fatalf("MirrorX Y = %.3f, want < -0.15 (upper band) for camera y=8..20", got.Y)
	}
}
