//go:build !dot

package robot

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

func TestTouchForeheadPettingAndCentralDoubleTap(t *testing.T) {
	now := time.Unix(1700000500, 0)
	r := newForTest(testDeps{now: func() time.Time { return now }})
	bounds := image.Rect(0, 0, 960, 480)

	// Swipe/drag below the top 35% forehead zone must NOT trigger petting.
	r.OnTouch(touch.Gesture{Kind: touch.SwipeRight, X: 480, Y: 260}, image.Pt(480, 260), bounds)
	if s := r.Snapshot(now); s.Mood != MoodCalm || s.PetGlow != 0 {
		t.Fatalf("below-forehead swipe mutated mood/glow: %+v", s)
	}

	// Horizontal swipe in top 35% forehead sets MoodHappy + PetGlow=1.0 decaying over ~2s.
	r.OnTouch(touch.Gesture{Kind: touch.SwipeLeft, X: 480, Y: 80}, image.Pt(480, 80), bounds)
	if s := r.Snapshot(now); s.Mood != MoodHappy || s.PetGlow != 1 {
		t.Fatalf("forehead swipe snap = %+v, want MoodHappy + PetGlow=1", s)
	}

	now = now.Add(1000 * time.Millisecond)
	mid := r.Snapshot(now)
	if mid.Mood != MoodHappy || math.Abs(mid.PetGlow-0.5) > 0.05 {
		t.Fatalf("at +1s expected MoodHappy + PetGlow~0.5, got mood=%q glow=%v", mid.Mood, mid.PetGlow)
	}

	now = now.Add(1100 * time.Millisecond) // +2.1s total
	done := r.Snapshot(now)
	if done.Mood != MoodCalm || done.PetGlow != 0 {
		t.Fatalf("at +2.1s expected MoodCalm + PetGlow=0, got %+v", done)
	}

	// Horizontal drag in forehead zone also triggers petting.
	r.OnTouch(touch.Gesture{Kind: touch.Drag, X: 300, Y: 70}, image.Pt(300, 70), bounds)
	r.OnTouch(touch.Gesture{Kind: touch.Drag, X: 325, Y: 72}, image.Pt(325, 72), bounds)
	r.OnTouch(touch.Gesture{Kind: touch.Release, X: 325, Y: 72}, image.Pt(325, 72), bounds)
	if s := r.Snapshot(now); s.Mood != MoodHappy || s.PetGlow != 1 {
		t.Fatalf("forehead horizontal drag snap = %+v, want MoodHappy + PetGlow=1", s)
	}
	now = now.Add(2200 * time.Millisecond) // wait for petting decay to clear

	// Single tap or taps >450ms apart in central 50% do NOT startle.
	r.OnTouch(touch.Gesture{Kind: touch.Tap, X: 480, Y: 240}, image.Pt(480, 240), bounds)
	now = now.Add(500 * time.Millisecond)
	r.OnTouch(touch.Gesture{Kind: touch.Tap, X: 480, Y: 240}, image.Pt(480, 240), bounds)
	if s := r.Snapshot(now); s.Mood != MoodCalm {
		t.Fatalf("slow double-tap startled unexpectedly: %+v", s)
	}

	// Double-tap outside central 50% does NOT startle.
	r.OnTouch(touch.Gesture{Kind: touch.Tap, X: 50, Y: 50}, image.Pt(50, 50), bounds)
	now = now.Add(150 * time.Millisecond)
	r.OnTouch(touch.Gesture{Kind: touch.Tap, X: 50, Y: 50}, image.Pt(50, 50), bounds)
	if s := r.Snapshot(now); s.Mood != MoodCalm {
		t.Fatalf("corner double-tap startled unexpectedly: %+v", s)
	}

	// Rapid double-tap (within 450ms) in central 50% triggers MoodStartled (~0.6s) -> MoodCurious (~1.5s).
	r.OnTouch(touch.Gesture{Kind: touch.Tap, X: 480, Y: 240}, image.Pt(480, 240), bounds)
	now = now.Add(200 * time.Millisecond)
	r.OnTouch(touch.Gesture{Kind: touch.Tap, X: 485, Y: 245}, image.Pt(485, 245), bounds)
	if s := r.Snapshot(now); s.Mood != MoodStartled {
		t.Fatalf("double-tap immediate mood = %q, want startled", s.Mood)
	}

	now = now.Add(300 * time.Millisecond) // +300ms (< 600ms)
	if s := r.Snapshot(now); s.Mood != MoodStartled {
		t.Fatalf("at +300ms mood = %q, want startled", s.Mood)
	}

	now = now.Add(400 * time.Millisecond) // +700ms (in [600ms, 2100ms))
	if s := r.Snapshot(now); s.Mood != MoodCurious {
		t.Fatalf("at +700ms mood = %q, want curious", s.Mood)
	}

	now = now.Add(1500 * time.Millisecond) // +2200ms (> 2100ms)
	if s := r.Snapshot(now); s.Mood != MoodCalm {
		t.Fatalf("at +2200ms mood = %q, want calm", s.Mood)
	}
}
