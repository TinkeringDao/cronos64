//go:build !dot

package robot

import (
	"image"
	"math"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/lenscover"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

func lensCovered() bool { return lenscover.Get().Covered() }

func listenLensCover(cb func()) func() {
	return lenscover.Get().Changed.Listen(func(bool) { cb() })
}

// OnTouch translates companion surface gestures into affective reactions:
//   - Horizontal swipe or drag inside the top 35% forehead zone sets MoodHappy + PetGlow=1.0
//     decaying smoothly over 2s.
//   - Rapid double-tap (<=450ms) inside the central 50% triggers MoodStartled (0.6s)
//     easing into MoodCurious (1.5s).
func (r *Robot) OnTouch(g touch.Gesture, at image.Point, bounds image.Rectangle) {
	if at == (image.Point{}) && (g.X != 0 || g.Y != 0) {
		at = image.Pt(g.X, g.Y)
	}
	if bounds.Empty() || !at.In(bounds) {
		return
	}

	relX := float64(at.X - bounds.Min.X)
	relY := float64(at.Y - bounds.Min.Y)
	w := float64(bounds.Dx())
	h := float64(bounds.Dy())

	inForehead := relY <= 0.35*h
	inCenter := relX >= 0.25*w && relX <= 0.75*w && relY >= 0.25*h && relY <= 0.75*h

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()

	switch g.Kind {
	case touch.SwipeLeft, touch.SwipeRight:
		r.dragActive = false
		if inForehead {
			r.petAt = now
			r.petUntil = now.Add(petDuration)
		}

	case touch.Drag:
		if !inForehead {
			r.dragActive = false
			return
		}
		if !r.dragActive {
			r.dragActive = true
			r.dragLastX = at.X
			r.dragLastY = at.Y
			return
		}
		dx := math.Abs(float64(at.X - r.dragLastX))
		dy := math.Abs(float64(at.Y - r.dragLastY))
		if dx >= 8 && dx >= dy {
			r.petAt = now
			r.petUntil = now.Add(petDuration)
			r.dragLastX = at.X
			r.dragLastY = at.Y
		}

	case touch.Release:
		r.dragActive = false

	case touch.Tap:
		r.dragActive = false
		if !inCenter {
			r.lastTapAt = now
			r.lastTapInCenter = false
			return
		}
		if !r.lastTapAt.IsZero() && r.lastTapInCenter && now.Sub(r.lastTapAt) <= doubleTapWindow {
			r.startledUntil = now.Add(startledDuration)
			r.curiousUntil = now.Add(startledDuration + curiousDuration)
			r.lastTapAt = now
			r.lastTapInCenter = false
			return
		}
		r.lastTapAt = now
		r.lastTapInCenter = true
	}
}
