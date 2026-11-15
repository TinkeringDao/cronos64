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

const (
	robotScreenID  = "robot"
	headTiltLevels = 9 // -6.0 deg to +6.0 deg in 1.5 deg steps
)

type bubbleStamp struct {
	img  *image.RGBA
	ox   int
	oy   int
	bhw  float64
	bhh  float64
	freq float64
	ph   float64
	bdx  float64
	bdy  float64
}

type robotRenderCache struct {
	size       image.Rectangle
	dirty      image.Rectangle
	barKey     string
	lastBar    []customZone
	themeKey   [5]any
	synthScale float64
	heads      [headTiltLevels]*image.RGBA
	headOX     int
	headOY     int
	bubbles    [5]bubbleStamp
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

func sdOrientedRRect(px, py, cx, cy, hx, hy, r, cosA, sinA float64) (float64, float64, float64) {
	dx, dy := px-cx, py-cy
	lx := dx*cosA + dy*sinA
	ly := -dx*sinA + dy*cosA
	qx := math.Abs(lx) - (hx - r)
	qy := math.Abs(ly) - (hy - r)
	out := math.Hypot(max(qx, 0.0), max(qy, 0.0)) + min(max(qx, qy), 0.0) - r
	return out, lx, ly
}

func sdTriangle2D(px, py, x0, y0, x1, y1, x2, y2 float64) float64 {
	e0x, e0y := x1-x0, y1-y0
	e1x, e1y := x2-x1, y2-y1
	e2x, e2y := x0-x2, y0-y2
	v0x, v0y := px-x0, py-y0
	v1x, v1y := px-x1, py-y1
	v2x, v2y := px-x2, py-y2
	pq0x := v0x - e0x*clamp01((v0x*e0x+v0y*e0y)/(e0x*e0x+e0y*e0y+1e-9))
	pq0y := v0y - e0y*clamp01((v0x*e0x+v0y*e0y)/(e0x*e0x+e0y*e0y+1e-9))
	pq1x := v1x - e1x*clamp01((v1x*e1x+v1y*e1y)/(e1x*e1x+e1y*e1y+1e-9))
	pq1y := v1y - e1y*clamp01((v1x*e1x+v1y*e1y)/(e1x*e1x+e1y*e1y+1e-9))
	pq2x := v2x - e2x*clamp01((v2x*e2x+v2y*e2y)/(e2x*e2x+e2y*e2y+1e-9))
	pq2y := v2y - e2y*clamp01((v2x*e2x+v2y*e2y)/(e2x*e2x+e2y*e2y+1e-9))
	s := 1.0
	if e0x*e2y-e0y*e2x < 0 {
		s = -1.0
	}
	d2 := min(pq0x*pq0x+pq0y*pq0y, min(pq1x*pq1x+pq1y*pq1y, pq2x*pq2x+pq2y*pq2y))
	cMin := min(s*(v0x*e0y-v0y*e0x), min(s*(v1x*e1y-v1y*e1x), s*(v2x*e2y-v2y*e2x)))
	if cMin > 0 {
		return -math.Sqrt(d2)
	}
	return math.Sqrt(d2)
}

func blendPre(dst *image.RGBA, x, y int, c color.RGBA, a float64) {
	if a <= 0 {
		return
	}
	a = clamp01(a)
	i := dst.PixOffset(x, y)
	da := float64(dst.Pix[i+3]) / 255.0
	outA := a + da*(1.0-a)
	if outA <= 1e-5 {
		return
	}
	r := (float64(c.R)*a + float64(dst.Pix[i+0])*da*(1.0-a)) / outA
	g := (float64(c.G)*a + float64(dst.Pix[i+1])*da*(1.0-a)) / outA
	b := (float64(c.B)*a + float64(dst.Pix[i+2])*da*(1.0-a)) / outA
	dst.Pix[i+0] = uint8(clamp01(r/255.0) * 255.0)
	dst.Pix[i+1] = uint8(clamp01(g/255.0) * 255.0)
	dst.Pix[i+2] = uint8(clamp01(b/255.0) * 255.0)
	dst.Pix[i+3] = uint8(clamp01(outA) * 255.0)
}

func ensureProceduralStamps(cache *robotRenderCache, sc float64) {
	if math.Abs(cache.synthScale-sc) < 1e-4 && cache.heads[0] != nil {
		return
	}
	cache.synthScale = sc
	darkInk := color.RGBA{20, 18, 26, 255}
	pureWhite := color.RGBA{255, 255, 255, 255}
	baseCream := color.RGBA{245, 236, 224, 255}
	warmPeach := color.RGBA{253, 229, 198, 255}
	coolShadow := color.RGBA{178, 190, 206, 255}

	// 1. Synthesize 9 tilted 3D-shaded pillow head shells (-6 deg to +6 deg).
	hx, hy, hr := 96.0*sc, 72.0*sc, 40.0*sc
	pad := int(math.Ceil(18.0 * sc))
	hw := int(math.Ceil(hx)) + pad
	hh := int(math.Ceil(hy)) + pad
	w, h := hw*2, hh*2
	cache.headOX, cache.headOY = hw, hh

	for idx := 0; idx < headTiltLevels; idx++ {
		deg := -6.0 + float64(idx)*1.5
		rad := deg * math.Pi / 180.0
		cosA, sinA := math.Cos(rad), math.Sin(rad)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		cx, cy := float64(hw), float64(hh)

		for y := 0; y < h; y++ {
			py := float64(y) + 0.5
			for x := 0; x < w; x++ {
				px := float64(x) + 0.5
				d, lx, ly := sdOrientedRRect(px, py, cx, cy, hx, hy, hr, cosA, sinA)
				if d > 8.5*sc {
					continue
				}
				if d <= 8.5*sc {
					blendPre(img, x, y, pureWhite, clamp01(0.5-(d-8.5*sc)))
				}
				if d <= 3.2*sc {
					blendPre(img, x, y, darkInk, clamp01(0.5-(d-3.2*sc)))
				}
				if d <= -1.4*sc {
					ux := (lx + hx) / (2.0 * hx)
					uy := (ly + hy) / (2.0 * hy)
					warmT := clamp01(ux*0.75 + (1.0-uy)*0.45 - 0.2)
					col := lerp(baseCream, warmPeach, warmT)

					dShadow, _, _ := sdOrientedRRect(
						px-16.5*sc*cosA,
						py+11.0*sc,
						cx, cy, hx-3.6*sc, hy-3.6*sc, hr-1.8*sc, cosA, sinA,
					)
					if dShadow > -3.6*sc {
						st := clamp01((dShadow + 3.6*sc) / (2.8 * sc))
						col = lerp(col, coolShadow, st*0.92)
					}

					if lx > 2.0*sc && ly < 2.0*sc {
						tx := clamp01((lx - 2.0*sc) / (16.0 * sc))
						ty := clamp01((-2.0*sc - ly) / (16.0 * sc))
						envH := min(tx, ty)
						dCurve := math.Abs(d+12.5*sc) - 4.8*sc*envH
						if dCurve < 1.0 && envH > 0.05 {
							col = lerp(col, pureWhite, clamp01(0.5-dCurve)*0.92)
						}
					}
					blendPre(img, x, y, col, clamp01(0.5-(d+1.4*sc)))
				}
			}
		}
		cache.heads[idx] = img
	}

	// 2. Synthesize the 5 seamless SDF speech-bubble shells (rounded rect + triangular pointer tail).
	specs := [5]struct {
		bdx, bdy, bhw, bhh, br, tbx, ttdx, ttdy, freq, ph float64
	}{
		{-128, -148, 70, 31, 15, 22, 35, 44, 0.36, 0.0},
		{-178, -48, 50, 28, 24, 16, 31, 37, 0.40, 1.4},
		{48, -152, 35, 20, 11, -11, -18, 31, 0.44, 2.7},
		{138, -102, 61, 24, 13, -31, -42, 37, 0.38, 0.9},
		{162, -29, 50, 28, 24, -18, -31, 37, 0.39, 3.6},
	}

	for i, sp := range specs {
		bhw, bhh, br := sp.bhw*sc, sp.bhh*sc, sp.br*sc
		tbx, ttdx, ttdy := sp.tbx*sc, sp.ttdx*sc, sp.ttdy*sc
		bpad := int(math.Ceil(10.0 * sc))
		minX := int(math.Floor(min(-bhw, ttdx))) - bpad
		maxX := int(math.Ceil(max(bhw, ttdx))) + bpad
		minY := int(math.Floor(-bhh)) - bpad
		maxY := int(math.Ceil(max(bhh, ttdy))) + bpad
		bw, bh := maxX-minX, maxY-minY
		ox, oy := -minX, -minY

		img := image.NewRGBA(image.Rect(0, 0, bw, bh))
		bcx, bcy := float64(ox), float64(oy)
		tx0, ty0 := bcx+tbx-10.0*sc, bcy+bhh-5.5*sc
		tx1, ty1 := bcx+tbx+10.0*sc, bcy+bhh-5.5*sc
		tx2, ty2 := bcx+ttdx, bcy+ttdy

		for y := 0; y < bh; y++ {
			py := float64(y) + 0.5
			for x := 0; x < bw; x++ {
				px := float64(x) + 0.5
				dr, _, _ := sdOrientedRRect(px, py, bcx, bcy, bhw, bhh, br, 1.0, 0.0)
				dt := sdTriangle2D(px, py, tx0, ty0, tx1, ty1, tx2, ty2) - 1.4*sc
				d := min(dr, dt)
				if d > 6.0*sc {
					continue
				}
				if d <= 6.0*sc {
					blendPre(img, x, y, pureWhite, clamp01(0.5-(d-6.0*sc)))
				}
				if d <= 2.0*sc {
					blendPre(img, x, y, darkInk, clamp01(0.5-(d-2.0*sc)))
				}
				if d <= -1.2*sc {
					blendPre(img, x, y, pureWhite, clamp01(0.5-(d+1.2*sc)))
				}
			}
		}

		cache.bubbles[i] = bubbleStamp{
			img:  img,
			ox:   ox,
			oy:   oy,
			bhw:  bhw,
			bhh:  bhh,
			freq: sp.freq / sc,
			ph:   sp.ph,
			bdx:  sp.bdx * sc,
			bdy:  sp.bdy * sc,
		}
	}
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

	paintArticulatedReachy(r, cache, snap, geom, s.now)
	cache.dirty = curDirty
	return cache.lastBar
}

type robotGeom struct {
	sc          float64
	cx0, cy0    float64
	tiltIdx     int
	tiltDeg     float64
	cosA, sinA  float64
	antSway     float64
	coilStretch float64
	blinkL      float64
	blinkR      float64
	dirty       image.Rectangle
}

func computeRobotGeom(r *renderer, snap robot.Snapshot, now time.Time) robotGeom {
	barH := r.s(customBarH)
	sc := float64(r.w) / float64(drawnFor) * 0.90

	sockX := snap.LookX * float64(r.s(28))
	sockY := snap.LookY * float64(r.s(10))

	tSec := float64(now.UnixMilli()%60000) / 1000.0
	breathY := 0.0
	idleTilt := 0.0
	if snap.Mood != robot.MoodSleepy {
		breathY = math.Sin(tSec*1.8) * float64(r.s(2))
		idleTilt = math.Sin(tSec*1.1) * 1.2
	}

	cx0 := float64(r.w)*0.5 + sockX
	cy0 := float64(barH) + float64(r.h-barH)*0.67 + sockY + breathY

	// Dynamic head-roll tilt driven by gaze, mood, and gentle breathing sway
	tiltDeg := -2.5 - snap.LookX*3.0 + idleTilt
	switch snap.Mood {
	case robot.MoodCurious:
		tiltDeg += 4.5
	case robot.MoodHappy:
		tiltDeg += math.Sin(tSec*6.0) * 3.0
	case robot.MoodStartled:
		tiltDeg = 0.0
	}
	tiltIdx := int(math.Round((tiltDeg + 6.0) / 1.5))
	if tiltIdx < 0 {
		tiltIdx = 0
	} else if tiltIdx >= headTiltLevels {
		tiltIdx = headTiltLevels - 1
	}
	snappedDeg := -6.0 + float64(tiltIdx)*1.5
	rad := snappedDeg * math.Pi / 180.0
	cosA, sinA := math.Cos(rad), math.Sin(rad)

	// Spring-whip antenna sway & coil stretch
	antSway := -snap.LookX*0.55 + math.Sin(tSec*3.2)*0.22
	coilStretch := 1.0 + 0.05*math.Sin(tSec*3.6)
	switch snap.Mood {
	case robot.MoodListening:
		antSway += math.Sin(tSec*9.0) * (0.18 + 0.25*clamp01(snap.ListenPulse))
		coilStretch = 1.08 + 0.10*clamp01(snap.ListenPulse)
	case robot.MoodSpeaking:
		antSway += math.Sin(tSec*10.0) * (0.15 + 0.30*clamp01(snap.MouthOpen))
		coilStretch = 1.05 + 0.12*clamp01(snap.MouthOpen)
	case robot.MoodStartled:
		coilStretch = 1.18
	case robot.MoodHappy:
		antSway += math.Sin(tSec*8.0) * 0.35
		coilStretch = 1.10
	case robot.MoodSleepy:
		antSway = 0.05
		coilStretch = 0.90
	}

	// Natural blink + occasional double-blink + curious asymmetric squint
	var blinkL, blinkR float64
	if snap.Mood != robot.MoodStartled && snap.Mood != robot.MoodHappy && snap.Mood != robot.MoodSleepy {
		cycleMS := now.UnixMilli() % 4400
		if cycleMS >= 0 && cycleMS < 140 {
			blinkL = math.Sin(float64(cycleMS) / 140.0 * math.Pi)
			blinkR = blinkL
		} else if cycleMS >= 220 && cycleMS < 350 && (now.UnixMilli()/4400)%3 == 0 {
			// Every 3rd blink cycle performs a natural double-blink
			blinkL = math.Sin(float64(cycleMS-220) / 130.0 * math.Pi)
			blinkR = blinkL
		}
		if snap.Mood == robot.MoodCurious {
			// Inquisitive asymmetric squint on smaller right lens
			blinkR = max(blinkR, 0.38)
		}
	}

	halfSpanX := int(math.Ceil(252.0 * sc))
	topSpanY := int(math.Ceil(242.0 * sc))
	botSpanY := int(math.Ceil(96.0 * sc))

	dirty := image.Rect(
		int(math.Floor(cx0))-halfSpanX,
		int(math.Floor(cy0))-topSpanY,
		int(math.Ceil(cx0))+halfSpanX,
		int(math.Ceil(cy0))+botSpanY,
	).Intersect(image.Rect(0, barH, r.w, r.h))

	return robotGeom{
		sc:          sc,
		cx0:         cx0,
		cy0:         cy0,
		tiltIdx:     tiltIdx,
		tiltDeg:     snappedDeg,
		cosA:        cosA,
		sinA:        sinA,
		antSway:     antSway,
		coilStretch: coilStretch,
		blinkL:      blinkL,
		blinkR:      blinkR,
		dirty:       dirty,
	}
}

func paintArticulatedReachy(r *renderer, cache *robotRenderCache, snap robot.Snapshot, g robotGeom, now time.Time) {
	ensureProceduralStamps(cache, g.sc)

	tSec := float64(now.UnixMilli()%60000) / 1000.0
	darkInk := color.RGBA{20, 18, 26, 255}
	pureWhite := color.RGBA{255, 255, 255, 255}

	// 1. Orbiting Floating Speech Bubbles + Live 60 Hz Multi-Harmonic Audio Waveforms
	paintOrbitingSpeechBubbles(r, cache, snap, g, tSec)

	// 2. Spring-Physics Articulated Twin Helical-Coil Antennas + Signal Rings (in front of top bubbles)
	paintSpringAntennas(r, snap, g, tSec, darkInk, pureWhite)

	// 3. 3D-Shaded Tilted Pillow Head Shell (with cool blue-grey bevel shadow & top-right contour gloss)
	headImg := cache.heads[g.tiltIdx]
	if headImg != nil {
		blitRGBA(r.dst, headImg, int(math.Round(g.cx0))-cache.headOX, int(math.Round(g.cy0))-cache.headOY, g.dirty)
	}

	// 4. Forehead Pet Rosy/Amber Cheek Glow on Cream Face
	rotPt := func(lx, ly float64) (float64, float64) {
		return g.cx0 + lx*g.cosA - ly*g.sinA, g.cy0 + lx*g.sinA + ly*g.cosA
	}
	if snap.PetGlow > 0.01 {
		cheekCol := color.RGBA{242, 148, 110, uint8(clamp01(snap.PetGlow) * 140)}
		clx, cly := rotPt(-54.0*g.sc, 28.0*g.sc)
		crx, cry := rotPt(76.0*g.sc, 24.0*g.sc)
		drawAlphaDisc(r, clx, cly, 16.0*g.sc, cheekCol)
		drawAlphaDisc(r, crx, cry, 13.5*g.sc, cheekCol)
	}

	// 5. 2.5D Parallax Bronze Spectacle Bridge + Asymmetric Round Camera Lenses
	pShiftX := snap.LookX * 13.0 * g.sc
	pShiftY := snap.LookY * 7.5 * g.sc
	elx, ely := rotPt(-20.0*g.sc+pShiftX, 8.0*g.sc+pShiftY)
	erx, ery := rotPt(54.0*g.sc+pShiftX, 4.0*g.sc+pShiftY)
	rL := 32.5 * g.sc
	rR := 25.0 * g.sc

	// Bronze spectacle nose bridge bar with center vertical notch
	bx0, by0 := elx+18.0*g.sc*g.cosA, ely+18.0*g.sc*g.sinA
	bx1, by1 := erx-14.0*g.sc*g.cosA, ery-14.0*g.sc*g.sinA
	r.aaLine(bx0, by0, bx1, by1, 11.5*g.sc, darkInk)
	r.aaLine(bx0, by0, bx1, by1, 6.2*g.sc, color.RGBA{92, 62, 44, 255})
	mbx, mby := (bx0+bx1)*0.5, (by0+by1)*0.5
	r.aaLine(mbx-2.0*g.sc*g.sinA, mby-3.8*g.sc*g.cosA, mbx+2.0*g.sc*g.sinA, mby+3.8*g.sc*g.cosA, 2.2*g.sc, darkInk)

	// Draw left (larger) and right (smaller) optical spectacle lenses
	paintOpticalLens(r, elx, ely, rL, g.blinkL, snap, g, darkInk, pureWhite)
	paintOpticalLens(r, erx, ery, rR, g.blinkR, snap, g, darkInk, pureWhite)
}

func paintOrbitingSpeechBubbles(r *renderer, cache *robotRenderCache, snap robot.Snapshot, g robotGeom, tSec float64) {
	cPurple := color.RGBA{112, 90, 162, 255}
	cMagenta := color.RGBA{168, 102, 148, 255}
	cCoral := color.RGBA{222, 126, 78, 255}
	cAmber := color.RGBA{242, 168, 76, 255}

	speed := 6.0
	ampScale := 0.82
	bobAmp := 3.5 * g.sc

	switch snap.Mood {
	case robot.MoodListening:
		speed = 11.5
		ampScale = 0.78 + 0.50*clamp01(snap.ListenPulse)
		bobAmp = 5.0 * g.sc
	case robot.MoodSpeaking:
		speed = 13.5
		ampScale = 0.70 + 0.60*clamp01(snap.MouthOpen)
		bobAmp = 5.5 * g.sc
	case robot.MoodThinking:
		speed = 9.5
		ampScale = 0.90
	case robot.MoodStartled:
		speed = 15.0
		ampScale = 1.15
	case robot.MoodSleepy:
		speed = 2.0
		ampScale = 0.25
		bobAmp = 1.5 * g.sc
	}

	strokeW := max(1.8, 2.3*g.sc)

	for i := range cache.bubbles {
		b := &cache.bubbles[i]
		bobX := math.Sin(tSec*1.9+float64(i)*1.3) * bobAmp
		bobY := math.Cos(tSec*2.3+float64(i)*1.7) * bobAmp
		bcx := g.cx0 + b.bdx + bobX
		bcy := g.cy0 + b.bdy + bobY

		blitRGBA(r.dst, b.img, int(math.Round(bcx))-b.ox, int(math.Round(bcy))-b.oy, g.dirty)

		xStart := bcx - b.bhw + 13.0*g.sc
		xEnd := bcx + b.bhw - 13.0*g.sc
		span := max(1.0, xEnd-xStart)
		maxAmp := (b.bhh - 8.5*g.sc) * ampScale

		var prevX, prevY float64
		step := max(1.2, 1.6*g.sc)
		for x := xStart; x <= xEnd; x += step {
			u := (x - xStart) / span
			env := math.Sin(u * math.Pi)
			lx := x - xStart
			w1 := math.Sin(lx*b.freq - tSec*speed + b.ph)
			w2 := 0.65 * math.Sin(lx*b.freq*1.85+tSec*speed*0.7+b.ph*1.7)
			w3 := 0.35 * math.Cos(lx*b.freq*0.55-tSec*speed*0.4+b.ph)
			y := bcy + (w1+w2+w3)*0.48*maxAmp*env

			var col color.RGBA
			if u < 0.33 {
				col = lerp(cPurple, cMagenta, u/0.33)
			} else if u < 0.66 {
				col = lerp(cMagenta, cCoral, (u-0.33)/0.33)
			} else {
				col = lerp(cCoral, cAmber, (u-0.66)/0.34)
			}
			if x > xStart {
				r.aaLine(prevX, prevY, x, y, strokeW, col)
			}
			prevX, prevY = x, y
		}
	}
}

func paintSpringAntennas(r *renderer, snap robot.Snapshot, g robotGeom, tSec float64, darkInk, pureWhite color.RGBA) {
	rotPt := func(lx, ly float64) (float64, float64) {
		return g.cx0 + lx*g.cosA - ly*g.sinA, g.cy0 + lx*g.sinA + ly*g.cosA
	}

	antennas := [2]struct {
		side             float64
		mountLX, mountLY float64
		coilLX, coilLY   float64
		baseAngDeg       float64
		length           float64
	}{
		{-1, -94.0 * g.sc, -31.0 * g.sc, -83.0 * g.sc, -85.0 * g.sc, -2.5 + g.antSway*7.5, 136.0 * g.sc * g.coilStretch},
		{+1, 50.0 * g.sc, -70.0 * g.sc, 57.0 * g.sc, -88.0 * g.sc, 15.5 + g.antSway*9.5, 136.0 * g.sc * g.coilStretch},
	}

	for _, a := range antennas {
		mx, my := rotPt(a.mountLX, a.mountLY)
		bx, by := rotPt(a.coilLX, a.coilLY)
		ang := (a.baseAngDeg + g.tiltDeg) * math.Pi / 180.0
		ux, uy := math.Sin(ang), -math.Cos(ang)
		pxN, pyN := -uy, ux

		whip := g.antSway * 9.0 * g.sc * a.side
		tx := bx + ux*a.length + pxN*whip
		ty := by + uy*a.length + pyN*whip
		tipBX := bx + ux*(a.length-24.0*g.sc) + pxN*whip*0.86
		tipBY := by + uy*(a.length-24.0*g.sc) + pyN*whip*0.86

		// Pass 1: White sticker halo along mount, 6 spring coils, upper rod, and capsule tip
		r.aaLine(mx, my, bx, by, 15.0*g.sc, pureWhite)
		r.aaLine(bx, by, tx, ty, 14.0*g.sc, pureWhite)
		for ci := 0; ci < 6; ci++ {
			frac := 0.04 + float64(ci)*0.042
			ccx := bx + ux*(a.length*frac) + pxN*(whip*frac*frac)
			ccy := by + uy*(a.length*frac) + pyN*(whip*frac*frac)
			r.aaLine(ccx-pxN*6.8*g.sc, ccy-pyN*6.8*g.sc, ccx+pxN*6.8*g.sc, ccy+pyN*6.8*g.sc, 11.5*g.sc, pureWhite)
		}
		r.aaLine(tipBX, tipBY, tx, ty, 17.5*g.sc, pureWhite)

		// Pass 2: Dark ink mount bracket, 6 helical spring coils, straight rod, and capsule tip
		if a.side < 0 {
			r.aaDisc(mx+2.0*g.sc, my+3.5*g.sc, 8.2*g.sc, darkInk)
			r.aaDisc(mx+3.0*g.sc, my+3.5*g.sc, 5.0*g.sc, color.RGBA{182, 192, 206, 255})
		} else {
			r.aaDisc(mx, my+2.0*g.sc, 6.0*g.sc, darkInk)
			r.aaDisc(mx, my+2.0*g.sc, 3.2*g.sc, color.RGBA{92, 62, 44, 255})
		}
		r.aaLine(mx, my, bx+ux*5.5*g.sc, by+uy*5.5*g.sc, 5.0*g.sc, darkInk)
		coilTopX := bx + ux*(a.length*0.28) + pxN*(whip*0.08)
		coilTopY := by + uy*(a.length*0.28) + pyN*(whip*0.08)
		r.aaLine(coilTopX, coilTopY, tx, ty, 3.8*g.sc, darkInk)
		for ci := 0; ci < 6; ci++ {
			frac := 0.04 + float64(ci)*0.042
			ccx := bx + ux*(a.length*frac) + pxN*(whip*frac*frac)
			ccy := by + uy*(a.length*frac) + pyN*(whip*frac*frac)
			r.aaLine(ccx-pxN*5.6*g.sc, ccy-pyN*5.6*g.sc, ccx+pxN*5.6*g.sc, ccy+pyN*5.6*g.sc, 3.8*g.sc, darkInk)
		}
		r.aaLine(tipBX, tipBY, tx, ty, 7.8*g.sc, darkInk)
		r.aaLine(tipBX+pxN*1.1*g.sc, tipBY+2.5*g.sc, tx+pxN*1.1*g.sc, ty-2.5*g.sc, 1.8*g.sc, color.RGBA{96, 102, 122, 255})

		// Active antenna signal pulse ring when listening, speaking, or thinking
		if snap.Mood == robot.MoodListening || snap.Mood == robot.MoodSpeaking || snap.Mood == robot.MoodThinking {
			wave := 0.5 + 0.5*math.Sin(tSec*8.0+a.side)
			rad := (9.0 + 7.0*wave) * g.sc
			sigCol := lerp(color.RGBA{168, 102, 148, 255}, color.RGBA{242, 168, 76, 255}, wave)
			r.aaRing(tx, ty, rad, 2.0*g.sc, 0, 2*math.Pi, sigCol)
		}
	}
}

func paintOpticalLens(r *renderer, lcx, lcy, lr, blink float64, snap robot.Snapshot, g robotGeom, darkInk, pureWhite color.RGBA) {
	creamLid := color.RGBA{246, 234, 214, 255}
	warmBronze := color.RGBA{88, 60, 44, 255}

	// Outer dark ink spectacle rim + gunmetal/bronze inner bevel + deep obsidian glass
	r.aaDisc(lcx, lcy, lr+3.0*g.sc, darkInk)
	r.aaDisc(lcx, lcy, lr+0.8*g.sc, color.RGBA{46, 40, 48, 255})
	r.aaDisc(lcx-1.4*g.sc, lcy-1.4*g.sc, lr, warmBronze)
	r.aaDisc(lcx, lcy, lr-2.0*g.sc, color.RGBA{12, 10, 16, 255})

	pX := snap.LookX * 8.0 * g.sc
	pY := snap.LookY * 6.0 * g.sc

	switch snap.Mood {
	case robot.MoodHappy:
		arcR := lr * 0.58
		r.aaRing(lcx+pX*0.4, lcy+arcR*0.25+pY*0.4, arcR, max(3.0, lr*0.18), math.Pi*1.18, math.Pi*1.82, pureWhite)
		return
	case robot.MoodSleepy:
		drawLensEyelid(r, lcx, lcy, lr-1.0*g.sc, 0.72, creamLid, darkInk)
		return
	}

	// Lower-left warm amber/brown internal lens reflection crescent
	refCX := lcx - lr*0.10 + pX*0.45
	refCY := lcy + lr*0.10 + pY*0.45
	r.aaRing(refCX, refCY, lr*0.60, max(2.4, lr*0.15), math.Pi*0.48, math.Pi*0.92, warmBronze)

	// Upper-left subtle warm dome sheen inside glass
	drawAlphaDisc(r, lcx-lr*0.12+pX*0.55, lcy-lr*0.18+pY*0.55, lr*0.32, color.RGBA{58, 40, 32, 150})

	scale := 1.0
	if snap.Mood == robot.MoodStartled {
		scale = 1.25
	} else if snap.Mood == robot.MoodCurious || snap.Mood == robot.MoodListening {
		scale = 1.12
	}

	// Primary upper-right glossy white bean specular catchlight
	hx := lcx + lr*0.22 + pX
	hy := lcy - lr*0.18 + pY
	br := lr * 0.20 * scale
	r.aaLine(hx-br*0.28, hy-br*0.28, hx+br*0.22, hy+br*0.28, br*1.65, pureWhite)

	// Secondary lower-right crisp white dot catchlight
	dx := lcx + lr*0.35 + pX*0.85
	dy := lcy + lr*0.24 + pY*0.85
	r.aaDisc(dx, dy, max(2.2, lr*0.10*scale), pureWhite)

	if blink > 0.05 {
		drawLensEyelid(r, lcx, lcy, lr-1.0*g.sc, blink, creamLid, darkInk)
	}
}

func drawLensEyelid(r *renderer, cx, cy, rad, closure float64, lidCol, lashCol color.RGBA) {
	closure = clamp01(closure)
	lidBottomY := cy - rad + 2.0*rad*closure
	r2 := rad * rad
	for y := int(math.Floor(cy - rad)); y <= int(math.Ceil(lidBottomY)); y++ {
		dy := float64(y) + 0.5 - cy
		if dy*dy >= r2 {
			continue
		}
		halfW := math.Sqrt(r2 - dy*dy)
		x0 := int(math.Floor(cx - halfW))
		x1 := int(math.Ceil(cx + halfW))
		col := lidCol
		if float64(y) >= lidBottomY-3.0 {
			col = lashCol
		}
		for x := x0; x <= x1; x++ {
			r.blendAt(x, y, col, 1.0)
		}
	}
}

func blitRGBA(dst *image.RGBA, src *image.RGBA, ox, oy int, clip image.Rectangle) {
	sb := src.Bounds()
	target := image.Rect(ox, oy, ox+sb.Dx(), oy+sb.Dy()).Intersect(clip).Intersect(dst.Bounds())
	if target.Empty() {
		return
	}
	for y := target.Min.Y; y < target.Max.Y; y++ {
		sy := y - oy
		sOff := sy * src.Stride
		dOff := (y - dst.Rect.Min.Y) * dst.Stride
		for x := target.Min.X; x < target.Max.X; x++ {
			sx := x - ox
			si := sOff + sx*4
			sa := uint32(src.Pix[si+3])
			if sa == 0 {
				continue
			}
			di := dOff + (x-dst.Rect.Min.X)*4
			if sa == 255 {
				dst.Pix[di+0] = src.Pix[si+0]
				dst.Pix[di+1] = src.Pix[si+1]
				dst.Pix[di+2] = src.Pix[si+2]
				dst.Pix[di+3] = 255
			} else {
				invA := 255 - sa
				dst.Pix[di+0] = uint8((uint32(src.Pix[si+0])*sa + uint32(dst.Pix[di+0])*invA) / 255)
				dst.Pix[di+1] = uint8((uint32(src.Pix[si+1])*sa + uint32(dst.Pix[di+1])*invA) / 255)
				dst.Pix[di+2] = uint8((uint32(src.Pix[si+2])*sa + uint32(dst.Pix[di+2])*invA) / 255)
				dst.Pix[di+3] = 255
			}
		}
	}
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
