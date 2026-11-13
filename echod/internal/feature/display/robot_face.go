//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/robot"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

const robotScreenID = "robot"

type robotRenderCache struct {
	size     image.Rectangle
	dirty    image.Rectangle
	barKey   string
	lastBar  []customZone
	themeKey [5]any
}

func init() {
	registerScreen(customScreen{
		id:         robotScreenID,
		label:      "Robot",
		rank:       1,
		duringTurn: true,
		follow:     true,
		keeps:      true,
		fill:       fillRobotScreen,
		draw:       drawRobotFace,
		gesture:    robotGesture,
		key:        nil,
		next:       func(scene) time.Duration { return 16 * time.Millisecond },
	})
}

func fillRobotScreen(d *Display, _ *customView) {
	if d == nil {
		return
	}
	d.mu.Lock()
	asleep := !d.on || d.nightDark
	d.mu.Unlock()
	robot.Get().SetAsleep(asleep)
}

func syncRobotTurn(phase string, now time.Time) {
	var speechLevel, listenLevel float64
	switch phase {
	case "listening":
		listenLevel = turnEnergy(eqFor("listening", false, now))
	case "replying", "lingering":
		speechLevel = turnEnergy(eqFor(phase, false, now))
	}
	robot.Get().ObserveTurn(phase, speechLevel, listenLevel, now)
}

func robotGesture(d *Display, g touch.Gesture, at image.Point) {
	bounds := image.Rect(0, 0, drawnFor, drawnFor/2)
	if d != nil {
		if d.r != nil && d.r.dst != nil {
			bounds = d.r.dst.Rect
		} else if d.dev != nil && d.dev.Canvas() != nil {
			bounds = d.dev.Canvas().Rect
		}
	}
	robot.Get().OnTouch(g, at, bounds)
}

func drawRobotFace(r *renderer, s scene) []customZone {
	var snap robot.Snapshot
	if v, ok := s.custom.data.(robot.Snapshot); ok {
		snap = v
	} else {
		snap = robot.Get().Snapshot(s.now)
	}

	cache := customCache(r, robotScreenID, func() *robotRenderCache { return &robotRenderCache{} })
	themeSig := [5]any{walnut, amber, cream, dim, drawnFont()}

	summary := ""
	if snap.ThermalDegraded {
		summary = "Thermal"
	}
	barKey := fmt.Sprintf("%s|%t|%s|%v", s.now.Format("15:04"), snap.ThermalDegraded, s.custom.name, themeSig)
	barH := r.s(customBarH)

	fullReset := !r.customKept || cache.size != r.dst.Rect || cache.themeKey != themeSig
	if fullReset {
		if r.customKept {
			fillCanvas(r.dst, walnut)
		}
		cache.lastBar = r.customBar(s, summary, snap.ThermalDegraded, nightNow(s.now))
		cache.barKey = barKey
		cache.size = r.dst.Rect
		cache.themeKey = themeSig
		cache.dirty = image.Rectangle{}
	} else if cache.barKey != barKey {
		barRect := image.Rect(0, 0, r.w, barH).Intersect(r.dst.Rect)
		draw.Draw(r.dst, barRect, image.NewUniform(walnut), image.Point{}, draw.Src)
		cache.lastBar = r.customBar(s, summary, snap.ThermalDegraded, nightNow(s.now))
		cache.barKey = barKey
	}

	geom := computeRobotGeom(r, snap, s.now)
	curDirty := geom.dirty.Intersect(image.Rect(0, barH, r.w, r.h))

	if r.customKept && !fullReset {
		clearRect := cache.dirty.Union(curDirty).Intersect(image.Rect(0, barH, r.w, r.h))
		if !clearRect.Empty() {
			draw.Draw(r.dst, clearRect, image.NewUniform(walnut), image.Point{}, draw.Src)
		}
	}

	paintRobotFace(r, snap, geom, s.now)
	cache.dirty = curDirty
	return cache.lastBar
}

type robotGeom struct {
	cxL, cxR, cy        float64
	eyeR                float64
	pupilX, pupilY      float64
	mouthCX, mouthCY    float64
	cheekLX, cheekRX    float64
	cheekY, cheekR      float64
	blinkFactor         float64 // 0 = wide open, 1 = closed blink
	dirty               image.Rectangle
}

func computeRobotGeom(r *renderer, snap robot.Snapshot, now time.Time) robotGeom {
	midX := float64(r.w) / 2
	barH := r.s(customBarH)
	baseOffset := float64(r.s(146))
	baseEyeY := float64(r.s(226))

	sockX := snap.LookX * float64(r.s(26))
	sockY := snap.LookY * float64(r.s(14))

	cxL := midX - baseOffset + sockX
	cxR := midX + baseOffset + sockX
	cy := baseEyeY + sockY

	eyeR := float64(r.s(46))
	if snap.Mood == robot.MoodStartled {
		eyeR *= 1.18
	}

	pupilX := snap.LookX * float64(r.s(16))
	pupilY := snap.LookY * float64(r.s(10))

	mouthCX := midX + sockX*0.55
	mouthCY := cy + float64(r.s(110))

	cheekLX := cxL - float64(r.s(42))
	cheekRX := cxR + float64(r.s(42))
	cheekY := cy + float64(r.s(54))
	cheekR := float64(r.s(24))

	var blink float64
	if snap.Mood != robot.MoodStartled && snap.Mood != robot.MoodHappy && snap.Mood != robot.MoodSleepy {
		cycleMS := now.UnixMilli() % 4200
		if cycleMS >= 0 && cycleMS < 140 {
			phase := float64(cycleMS) / 140.0
			blink = math.Sin(phase * math.Pi)
		}
	}

	pad := float64(r.s(14))
	browMaxY := eyeR*1.18 + float64(r.s(26))
	minX := min(cxL-eyeR*1.25, cheekLX-cheekR) - pad
	maxX := max(cxR+eyeR*1.25, cheekRX+cheekR) + pad
	minY := (cy - browMaxY) - pad
	maxY := max(mouthCY+float64(r.s(46)), cheekY+cheekR) + pad

	dirty := image.Rect(
		int(math.Floor(minX)),
		int(math.Floor(minY)),
		int(math.Ceil(maxX)),
		int(math.Ceil(maxY)),
	).Intersect(image.Rect(0, barH, r.w, r.h))

	return robotGeom{
		cxL:         cxL,
		cxR:         cxR,
		cy:          cy,
		eyeR:        eyeR,
		pupilX:      pupilX,
		pupilY:      pupilY,
		mouthCX:     mouthCX,
		mouthCY:     mouthCY,
		cheekLX:     cheekLX,
		cheekRX:     cheekRX,
		cheekY:      cheekY,
		cheekR:      cheekR,
		blinkFactor: blink,
		dirty:       dirty,
	}
}

func paintRobotFace(r *renderer, snap robot.Snapshot, g robotGeom, now time.Time) {
	// Forehead pet glow on cheeks.
	if snap.PetGlow > 0.01 {
		cheekCol := color.RGBA{amber.R, amber.G, amber.B, uint8(clamp01(snap.PetGlow) * 120)}
		drawAlphaDisc(r, g.cheekLX, g.cheekY, g.cheekR, cheekCol)
		drawAlphaDisc(r, g.cheekRX, g.cheekY, g.cheekR, cheekCol)
	}

	// Expressive eyes + pupils + brows.
	drawRobotEye(r, g.cxL, g.cy, -1, snap, g)
	drawRobotEye(r, g.cxR, g.cy, +1, snap, g)

	// Mouth & vocal/listening/thinking feedback.
	drawRobotMouth(r, snap, g, now)
}

func drawRobotEye(r *renderer, cx, cy float64, side float64, snap robot.Snapshot, g robotGeom) {
	strokeW := float64(r.s(7))
	browStroke := float64(r.s(5))
	eyeFill := cream
	browCol := lerp(dim, cream, 0.45)

	switch snap.Mood {
	case robot.MoodHappy:
		// Warm upward ^ ^ arcs.
		arcR := g.eyeR * 0.85
		r.aaRing(cx, cy+arcR*0.25, arcR, strokeW*1.15, math.Pi*1.18, math.Pi*1.82, amber)
		return

	case robot.MoodSleepy:
		// Soft drowsy downward crescent lids.
		arcR := g.eyeR * 0.85
		r.aaRing(cx, cy-arcR*0.22, arcR, strokeW, math.Pi*0.18, math.Pi*0.82, lerp(dim, cream, 0.7))
		return
	}

	if g.blinkFactor > 0.65 {
		w := g.eyeR * 0.85
		r.aaLine(cx-w, cy+float64(r.s(4)), cx+w, cy+float64(r.s(4)), strokeW, eyeFill)
		return
	}

	// Open eye capsule / disc with vertical blink squash.
	radY := g.eyeR * (1.0 - 0.85*g.blinkFactor)
	if g.blinkFactor <= 0.05 {
		r.aaDisc(cx, cy, g.eyeR, eyeFill)
	} else {
		box := image.Rect(
			int(math.Round(cx-g.eyeR)),
			int(math.Round(cy-radY)),
			int(math.Round(cx+g.eyeR)),
			int(math.Round(cy+radY)),
		)
		r.roundFill(box, min(g.eyeR, radY), eyeFill, eyeFill)
	}

	// Parallax pupil & warm iris catchlight.
	if g.blinkFactor < 0.35 {
		pRad := g.eyeR * 0.42
		if snap.Mood == robot.MoodStartled {
			pRad = g.eyeR * 0.28
		} else if snap.Mood == robot.MoodCurious || snap.Mood == robot.MoodListening {
			pRad = g.eyeR * 0.48
		}
		px := cx + g.pupilX
		py := cy + g.pupilY
		r.aaDisc(px, py, pRad, walnut)
		// Subtle catchlight dot inside upper-right pupil edge.
		r.aaDisc(px+pRad*0.32, py-pRad*0.32, max(2.0, pRad*0.26), cream)
	}

	// Expressive brows for startled / curious / listening.
	switch snap.Mood {
	case robot.MoodStartled:
		r.aaRing(cx, cy-g.eyeR*0.18, g.eyeR*1.05, browStroke, math.Pi*1.28, math.Pi*1.72, amber)
	case robot.MoodCurious:
		tilt := float64(r.s(7)) * side
		yBase := cy - g.eyeR*1.08
		r.aaLine(cx-g.eyeR*0.65, yBase+tilt, cx+g.eyeR*0.65, yBase-tilt, browStroke, browCol)
	case robot.MoodListening:
		yBase := cy - g.eyeR*1.12
		r.aaLine(cx-g.eyeR*0.55, yBase, cx+g.eyeR*0.55, yBase-float64(r.s(4)), browStroke, amber)
	}
}

func drawRobotMouth(r *renderer, snap robot.Snapshot, g robotGeom, now time.Time) {
	cx, cy := g.mouthCX, g.mouthCY
	strokeW := float64(r.s(6))

	switch snap.Mood {
	case robot.MoodHappy:
		rad := float64(r.s(30))
		r.aaRing(cx, cy-rad*0.35, rad, strokeW, math.Pi*0.2, math.Pi*0.8, amber)
		return

	case robot.MoodStartled:
		r.aaRing(cx, cy, float64(r.s(15)), strokeW, 0, 2*math.Pi, amber)
		return

	case robot.MoodSleepy:
		w := float64(r.s(16))
		r.aaLine(cx-w, cy, cx+w, cy, strokeW*0.85, dim)
		return

	case robot.MoodListening:
		// Concentric breathing pulse halo + attentive mouth bar.
		pulseR := float64(r.s(16)) + clamp01(snap.ListenPulse)*float64(r.s(22))
		alpha := 0.35 + 0.55*clamp01(snap.ListenPulse)
		pulseCol := color.RGBA{amber.R, amber.G, amber.B, uint8(alpha * 255)}
		drawAlphaRing(r, cx, cy, pulseR, float64(r.s(5)), pulseCol)
		w := float64(r.s(18)) + clamp01(snap.ListenPulse)*float64(r.s(12))
		r.aaLine(cx-w, cy, cx+w, cy, strokeW, cream)
		return

	case robot.MoodThinking:
		// Three rhythmic orbital dots beneath the eyes.
		sec := float64(now.UnixMilli()%2400) / 2400.0 * 2 * math.Pi
		dotBaseR := float64(r.s(7))
		spacing := float64(r.s(28))
		for i := -1; i <= 1; i++ {
			wave := 0.5 + 0.5*math.Sin(sec*2-float64(i)*1.1)
			col := lerp(dim, amber, wave)
			dx := cx + float64(i)*spacing
			dy := cy + float64(r.s(8))*math.Sin(sec*2-float64(i)*1.1)
			r.aaDisc(dx, dy, dotBaseR*(0.75+0.35*wave), col)
		}
		return

	case robot.MoodSpeaking:
		open := clamp01(snap.MouthOpen)
		halfW := float64(r.s(24)) + open*float64(r.s(14))
		halfH := float64(r.s(4)) + open*float64(r.s(18))
		box := image.Rect(
			int(math.Round(cx-halfW)),
			int(math.Round(cy-halfH)),
			int(math.Round(cx+halfW)),
			int(math.Round(cy+halfH)),
		)
		r.roundFill(box, min(halfW, halfH), amber, lerp(amber, walnut, 0.25))
		return
	}

	// MoodCalm / MoodCurious neutral gentle smile.
	w := float64(r.s(20))
	r.aaLine(cx-w, cy, cx+w, cy+float64(r.s(1)), strokeW*0.9, lerp(dim, cream, 0.65))
}

func drawAlphaDisc(r *renderer, cx, cy, rad float64, c color.RGBA) {
	aScale := float64(c.A) / 255.0
	rgb := color.RGBA{c.R, c.G, c.B, 255}
	for y := int(cy - rad - 1); y <= int(cy+rad+1); y++ {
		for x := int(cx - rad - 1); x <= int(cx+rad+1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) - rad
			edge := clamp01(0.5 - d)
			if edge > 0 {
				r.blendAt(x, y, rgb, edge*aScale)
			}
		}
	}
}

func drawAlphaRing(r *renderer, cx, cy, rad, w float64, c color.RGBA) {
	aScale := float64(c.A) / 255.0
	rgb := color.RGBA{c.R, c.G, c.B, 255}
	for y := int(cy - rad - w - 1); y <= int(cy+rad+w+1); y++ {
		for x := int(cx - rad - w - 1); x <= int(cx+rad+w+1); x++ {
			px, py := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Abs(math.Hypot(px, py)-rad) - w/2
			edge := clamp01(0.5 - d)
			if edge > 0 {
				r.blendAt(x, y, rgb, edge*aScale)
			}
		}
	}
}
