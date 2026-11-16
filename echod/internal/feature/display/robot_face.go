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
	headTiltLevels = 13 // -7.5 deg to +7.5 deg in 1.25 deg steps
)

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
	// Subtle pillow crown along top/bottom edges matching physical Reachy Mini shell
	nx := lx / max(1.0, hx)
	crown := 3.5 * (1.0 - min(1.0, nx*nx))
	qx := math.Abs(lx) - (hx - r)
	qy := math.Abs(ly) - (hy + crown - r)
	out := math.Hypot(max(qx, 0.0), max(qy, 0.0)) + min(max(qx, qy), 0.0) - r
	return out, lx, ly
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
	baseCream := color.RGBA{246, 239, 229, 255}
	warmPeach := color.RGBA{253, 233, 206, 255}
	coolShadow := color.RGBA{176, 188, 206, 255}

	// Synthesize 13 tilted 3D-shaded Reachy Mini pillow head shells (-7.5 deg to +7.5 deg in 1.25 deg steps).
	// Hero-sized head (no speech bubbles around it).
	hx, hy, hr := 130.0*sc, 92.0*sc, 50.0*sc
	pad := int(math.Ceil(22.0 * sc))
	hw := int(math.Ceil(hx)) + pad
	hh := int(math.Ceil(hy)) + pad
	w, h := hw*2, hh*2
	cache.headOX, cache.headOY = hw, hh

	for idx := 0; idx < headTiltLevels; idx++ {
		deg := -7.5 + float64(idx)*1.25
		rad := deg * math.Pi / 180.0
		cosA, sinA := math.Cos(rad), math.Sin(rad)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		cx, cy := float64(hw), float64(hh)

		for y := 0; y < h; y++ {
			py := float64(y) + 0.5
			for x := 0; x < w; x++ {
				px := float64(x) + 0.5
				d, lx, ly := sdOrientedRRect(px, py, cx, cy, hx, hy, hr, cosA, sinA)
				if d > 8.0*sc {
					continue
				}
				if d <= 8.0*sc {
					blendPre(img, x, y, pureWhite, clamp01(0.5-(d-8.0*sc)))
				}
				if d <= 3.2*sc {
					blendPre(img, x, y, darkInk, clamp01(0.5-(d-3.2*sc)))
				}
				if d <= -1.4*sc {
					ux := (lx + hx) / (2.0 * hx)
					uy := (ly + hy) / (2.0 * hy)
					warmT := clamp01(ux*0.75 + (1.0-uy)*0.45 - 0.18)
					col := lerp(baseCream, warmPeach, warmT)

					// Left & bottom 3D bevel shadow matching physical Reachy Mini pillow shell
					dShadow, _, _ := sdOrientedRRect(
						px-20.0*sc*cosA,
						py+13.5*sc,
						cx, cy, hx-4.2*sc, hy-4.2*sc, hr-2.0*sc, cosA, sinA,
					)
					if dShadow > -4.2*sc {
						st := clamp01((dShadow + 4.2*sc) / (3.2 * sc))
						col = lerp(col, coolShadow, st*0.92)
					}

					// Top-right curved specular studio gloss highlight
					if lx > 4.0*sc && ly < 4.0*sc {
						tx := clamp01((lx - 4.0*sc) / (22.0 * sc))
						ty := clamp01((-4.0*sc - ly) / (22.0 * sc))
						envH := min(tx, ty)
						dCurve := math.Abs(d+15.5*sc) - 5.8*sc*envH
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
	nodPitchY   float64
	turnYawX    float64
	talkEnergy  float64
	listenLevel float64
	antWhipL    float64
	antWhipR    float64
	coilStretch float64
	blinkL      float64
	blinkR      float64
	lowerSmile  float64
	dirty       image.Rectangle
}

func computeRobotGeom(r *renderer, snap robot.Snapshot, now time.Time) robotGeom {
	barH := r.s(customBarH)
	sc := float64(r.w) / float64(drawnFor) * 0.92

	tSec := float64(now.UnixMilli()%60000) / 1000.0

	// Voice / conversational envelope for talking and listening head micro-animations
	talkEnergy := clamp01(snap.MouthOpen)
	if snap.Mood == robot.MoodSpeaking && talkEnergy < 0.25 {
		// Ensure continuous syllabic modulation even between envelope peaks during speaking turns
		talkEnergy = 0.25 + 0.35*(0.5+0.5*math.Sin(tSec*11.5)*math.Cos(tSec*4.7))
	}
	listenLevel := clamp01(snap.ListenPulse)
	if snap.Mood == robot.MoodListening && listenLevel < 0.25 {
		listenLevel = 0.25 + 0.25*(0.5+0.5*math.Sin(tSec*6.5))
	}

	// Multi-harmonic syllabic head nodding (pitch), conversational head turns (yaw), and head tilts (roll)
	syllableNod := talkEnergy * (0.62*math.Sin(tSec*13.0) + 0.38*math.Cos(tSec*8.3+0.7))
	syllableYaw := talkEnergy * (0.55*math.Sin(tSec*5.4+0.3) + 0.45*math.Cos(tSec*3.7))
	syllableTilt := talkEnergy * (0.60*math.Sin(tSec*6.8+0.4) + 0.40*math.Cos(tSec*4.1))

	breathY := 0.0
	idleTilt := 0.0
	if snap.Mood != robot.MoodSleepy {
		breathY = math.Sin(tSec*1.8) * float64(r.s(2))
		idleTilt = math.Sin(tSec*1.1) * 1.3
	}

	sockX := (snap.LookX + syllableYaw*0.32) * float64(r.s(30))
	sockY := (snap.LookY + syllableNod*0.42) * float64(r.s(12))

	cx0 := float64(r.w)*0.5 + sockX
	cy0 := float64(barH) + float64(r.h-barH)*0.63 + sockY + breathY

	// Dynamic head-roll tilt driven by gaze, mood, listening head-cock, and talking cadence
	tiltDeg := -2.0 - snap.LookX*3.2 + idleTilt + syllableTilt*4.8
	nodPitchY := snap.LookY*9.5*sc + syllableNod*9.0*sc
	turnYawX := snap.LookX*15.5*sc + syllableYaw*7.5*sc

	switch snap.Mood {
	case robot.MoodCurious:
		tiltDeg += 5.0
		nodPitchY -= 3.0 * sc
	case robot.MoodListening:
		// Attentive head cock & slight upward perk while listening
		tiltDeg += 3.8 + 1.5*math.Sin(tSec*3.2)
		nodPitchY -= (3.5 + 2.5*listenLevel) * sc
	case robot.MoodThinking:
		// Thoughtful side tilt & upward pondering gaze
		tiltDeg += 4.5 * math.Sin(tSec*2.2)
		nodPitchY -= 5.5 * sc
		turnYawX += 7.0 * math.Cos(tSec*1.8) * sc
	case robot.MoodHappy:
		tiltDeg += math.Sin(tSec*6.0) * 3.8
		nodPitchY -= 2.5 * sc
	case robot.MoodStartled:
		tiltDeg = 0.0
		nodPitchY -= 5.0 * sc
	case robot.MoodSleepy:
		tiltDeg -= 3.2
		nodPitchY += 7.0 * sc
	}
	if snap.PetGlow > 0.01 {
		tiltDeg += 4.5 * snap.PetGlow * math.Sin(tSec*5.5)
	}

	tiltIdx := int(math.Round((tiltDeg + 7.5) / 1.25))
	if tiltIdx < 0 {
		tiltIdx = 0
	} else if tiltIdx >= headTiltLevels {
		tiltIdx = headTiltLevels - 1
	}
	snappedDeg := -7.5 + float64(tiltIdx)*1.25
	rad := snappedDeg * math.Pi / 180.0
	cosA, sinA := math.Cos(rad), math.Sin(rad)

	// Independent left/right spring-whip antenna kinematics & helical coil compression/extension
	baseSway := -snap.LookX*0.55 + math.Sin(tSec*3.1)*0.20
	antWhipL := baseSway
	antWhipR := baseSway
	coilStretch := 1.0 + 0.05*math.Sin(tSec*3.6)

	switch snap.Mood {
	case robot.MoodListening:
		// Antennas perk inward attentively with high-frequency micro-tremor
		perk := 0.25 + 0.25*listenLevel
		antWhipL += perk + math.Sin(tSec*14.0)*(0.14+0.20*listenLevel)
		antWhipR -= perk + math.Cos(tSec*14.0)*(0.14+0.20*listenLevel)
		coilStretch = 1.08 + 0.10*listenLevel
	case robot.MoodThinking:
		// Alternating pondering antenna twitches
		antWhipL += 0.42 * math.Sin(tSec*4.5)
		antWhipR += 0.42 * math.Cos(tSec*4.5)
		coilStretch = 1.04 + 0.07*math.Sin(tSec*5.0)
	case robot.MoodSpeaking:
		// Lively counter-harmonic syllabic whip & spring bounce while speaking
		antWhipL += talkEnergy * (0.48*math.Sin(tSec*12.5) + 0.28*math.Cos(tSec*19.0))
		antWhipR += talkEnergy * (-0.48*math.Sin(tSec*12.5+0.4) + 0.28*math.Sin(tSec*17.5))
		coilStretch = 1.04 + 0.16*talkEnergy*(0.6+0.4*math.Sin(tSec*15.0))
	case robot.MoodStartled:
		coilStretch = 1.18
		antWhipL = -0.35
		antWhipR = 0.35
	case robot.MoodHappy:
		antWhipL += math.Sin(tSec*8.5) * 0.42
		antWhipR -= math.Sin(tSec*8.5) * 0.42
		coilStretch = 1.12
	case robot.MoodSleepy:
		antWhipL = -0.30
		antWhipR = 0.30
		coilStretch = 0.88
	}
	if talkEnergy > 0.02 && snap.Mood != robot.MoodSpeaking {
		antWhipL += talkEnergy * 0.40 * math.Sin(tSec*12.5)
		antWhipR -= talkEnergy * 0.40 * math.Sin(tSec*12.5)
		coilStretch += 0.12 * talkEnergy
	}

	// Natural blink + occasional double-blink + curious squint + talking lower-lid cheek smile
	var blinkL, blinkR, lowerSmile float64
	if snap.Mood != robot.MoodStartled && snap.Mood != robot.MoodHappy && snap.Mood != robot.MoodSleepy {
		cycleMS := now.UnixMilli() % 4200
		if cycleMS >= 0 && cycleMS < 140 {
			blinkL = math.Sin(float64(cycleMS) / 140.0 * math.Pi)
			blinkR = blinkL
		} else if cycleMS >= 220 && cycleMS < 350 && (now.UnixMilli()/4200)%3 == 0 {
			blinkL = math.Sin(float64(cycleMS-220) / 130.0 * math.Pi)
			blinkR = blinkL
		}
		if snap.Mood == robot.MoodCurious {
			blinkR = max(blinkR, 0.36)
		}
	}
	if talkEnergy > 0.03 {
		// Engaged lower-lid cheek crinkle that lifts rhythmically on spoken syllables
		lowerSmile = clamp01(0.12 + 0.26*talkEnergy*(0.65+0.35*math.Sin(tSec*13.0)))
	} else if listenLevel > 0.05 {
		lowerSmile = clamp01(0.08 * listenLevel)
	}

	halfSpanX := int(math.Ceil(215.0 * sc))
	topSpanY := int(math.Ceil(268.0 * sc))
	botSpanY := int(math.Ceil(126.0 * sc))

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
		nodPitchY:   nodPitchY,
		turnYawX:    turnYawX,
		talkEnergy:  talkEnergy,
		listenLevel: listenLevel,
		antWhipL:    antWhipL,
		antWhipR:    antWhipR,
		coilStretch: coilStretch,
		blinkL:      blinkL,
		blinkR:      blinkR,
		lowerSmile:  lowerSmile,
		dirty:       dirty,
	}
}

func paintArticulatedReachy(r *renderer, cache *robotRenderCache, snap robot.Snapshot, g robotGeom, now time.Time) {
	ensureProceduralStamps(cache, g.sc)

	tSec := float64(now.UnixMilli()%60000) / 1000.0
	darkInk := color.RGBA{20, 18, 26, 255}
	pureWhite := color.RGBA{255, 255, 255, 255}

	// 1. Spring-Physics Articulated Twin Helical-Coil Antennas (no floating speech bubbles!)
	paintSpringAntennas(r, snap, g, tSec, darkInk, pureWhite)

	// 2. 3D-Shaded Tilted Pillow Head Shell (with cool blue-grey bevel shadow & top-right contour gloss)
	headImg := cache.heads[g.tiltIdx]
	if headImg != nil {
		blitRGBA(r.dst, headImg, int(math.Round(g.cx0))-cache.headOX, int(math.Round(g.cy0))-cache.headOY, g.dirty)
	}

	rotPt := func(lx, ly float64) (float64, float64) {
		return g.cx0 + lx*g.cosA - ly*g.sinA, g.cy0 + lx*g.sinA + ly*g.cosA
	}

	// 3. Forehead Pet Rosy/Amber Cheek Glow on Cream Face
	if snap.PetGlow > 0.01 {
		cheekCol := color.RGBA{242, 148, 110, uint8(clamp01(snap.PetGlow) * 145)}
		clx, cly := rotPt(-72.0*g.sc+g.turnYawX*0.5, 34.0*g.sc+g.nodPitchY*0.6)
		crx, cry := rotPt(96.0*g.sc+g.turnYawX*0.5, 28.0*g.sc+g.nodPitchY*0.6)
		drawAlphaDisc(r, clx, cly, 20.0*g.sc, cheekCol)
		drawAlphaDisc(r, crx, cry, 17.0*g.sc, cheekCol)
	}

	// 4. 2.5D Parallax Spectacle Bridge + Asymmetric Round Camera Lenses
	// Spectacles sit on the front surface of the curved head, shifting with 3D yaw and pitch nod!
	elx, ely := rotPt(-28.0*g.sc+g.turnYawX, 8.0*g.sc+g.nodPitchY)
	erx, ery := rotPt(68.0*g.sc+g.turnYawX, 3.0*g.sc+g.nodPitchY)

	// Subtle 3D perspective foreshortening of left/right lens radii as the head turns left/right
	yawNorm := clamp01((snap.LookX+1.0)*0.5) - 0.5 // -0.5 .. +0.5
	rL := 42.5 * g.sc * (1.0 - yawNorm*0.08)
	rR := 33.0 * g.sc * (1.0 + yawNorm*0.08)

	// Spectacle nose bridge bar with center optical sensor dot (matching real Reachy Mini hardware)
	bx0, by0 := elx+24.0*g.sc*g.cosA, ely+24.0*g.sc*g.sinA
	bx1, by1 := erx-18.0*g.sc*g.cosA, ery-18.0*g.sc*g.sinA
	r.aaLine(bx0, by0, bx1, by1, 14.5*g.sc, darkInk)
	r.aaLine(bx0, by0, bx1, by1, 8.0*g.sc, color.RGBA{92, 62, 44, 255})
	mbx, mby := (bx0+bx1)*0.5, (by0+by1)*0.5
	r.aaLine(mbx-2.4*g.sc*g.sinA, mby-4.8*g.sc*g.cosA, mbx+2.4*g.sc*g.sinA, mby+4.8*g.sc*g.cosA, 2.6*g.sc, darkInk)

	// Center bridge optical sensor dot — pulses with syllabic voice luminance when talking or listening
	if g.talkEnergy > 0.02 || g.listenLevel > 0.02 {
		pulse := clamp01(g.talkEnergy*(0.65+0.35*math.Sin(tSec*14.0)) + g.listenLevel*0.75)
		dotCol := lerp(color.RGBA{120, 95, 75, 255}, color.RGBA{255, 196, 110, 255}, pulse)
		r.aaDisc(mbx, mby, (2.2+1.2*pulse)*g.sc, dotCol)
	}

	// Draw left (larger) and right (smaller) optical spectacle lenses with talking iris & eyelid micro-animations
	paintOpticalLens(r, elx, ely, rL, g.blinkL, snap, g, tSec, darkInk, pureWhite)
	paintOpticalLens(r, erx, ery, rR, g.blinkR, snap, g, tSec, darkInk, pureWhite)
}

func paintSpringAntennas(r *renderer, snap robot.Snapshot, g robotGeom, tSec float64, darkInk, pureWhite color.RGBA) {
	// Antenna mounts experience slight counter-parallax when head pitches forward/backward
	pitchCounterY := -g.nodPitchY * 0.28
	yawCounterX := -g.turnYawX * 0.22
	rotPt := func(lx, ly float64) (float64, float64) {
		lx += yawCounterX
		ly += pitchCounterY
		return g.cx0 + lx*g.cosA - ly*g.sinA, g.cy0 + lx*g.sinA + ly*g.cosA
	}

	antennas := [2]struct {
		side             float64
		mountLX, mountLY float64
		coilLX, coilLY   float64
		baseAngDeg       float64
		whip             float64
		length           float64
	}{
		{-1, -124.0 * g.sc, -42.0 * g.sc, -110.0 * g.sc, -108.0 * g.sc, -2.5 + g.antWhipL*9.5, g.antWhipL, 146.0 * g.sc * g.coilStretch},
		{+1, 68.0 * g.sc, -90.0 * g.sc, 77.0 * g.sc, -112.0 * g.sc, 14.5 + g.antWhipR*11.0, g.antWhipR, 146.0 * g.sc * g.coilStretch},
	}

	for _, a := range antennas {
		mx, my := rotPt(a.mountLX, a.mountLY)
		bx, by := rotPt(a.coilLX, a.coilLY)
		ang := (a.baseAngDeg + g.tiltDeg) * math.Pi / 180.0
		ux, uy := math.Sin(ang), -math.Cos(ang)
		pxN, pyN := -uy, ux

		whipPx := a.whip * 12.5 * g.sc
		tx := bx + ux*a.length + pxN*whipPx
		ty := by + uy*a.length + pyN*whipPx
		tipBX := bx + ux*(a.length-26.0*g.sc) + pxN*whipPx*0.86
		tipBY := by + uy*(a.length-26.0*g.sc) + pyN*whipPx*0.86

		// Pass 1: Crisp white sticker border along mount, 7 helical spring coils, upper whip rod, and capsule tip
		r.aaLine(mx, my, bx, by, 16.5*g.sc, pureWhite)
		r.aaLine(bx, by, tx, ty, 15.0*g.sc, pureWhite)
		for ci := 0; ci < 7; ci++ {
			frac := 0.04 + float64(ci)*0.038
			ccx := bx + ux*(a.length*frac) + pxN*(whipPx*frac*frac)
			ccy := by + uy*(a.length*frac) + pyN*(whipPx*frac*frac)
			r.aaLine(ccx-pxN*7.6*g.sc, ccy-pyN*7.6*g.sc, ccx+pxN*7.6*g.sc, ccy+pyN*7.6*g.sc, 12.2*g.sc, pureWhite)
		}
		r.aaLine(tipBX, tipBY, tx, ty, 18.5*g.sc, pureWhite)

		// Pass 2: Dark ink mount bracket, 7 helical spring coils with metallic glint, whip rod, and capsule tip
		if a.side < 0 {
			r.aaDisc(mx+2.5*g.sc, my+4.0*g.sc, 9.8*g.sc, darkInk)
			r.aaDisc(mx+3.5*g.sc, my+4.0*g.sc, 6.0*g.sc, color.RGBA{182, 192, 206, 255})
		} else {
			r.aaDisc(mx, my+2.2*g.sc, 7.2*g.sc, darkInk)
			r.aaDisc(mx, my+2.2*g.sc, 4.0*g.sc, color.RGBA{92, 62, 44, 255})
		}
		r.aaLine(mx, my, bx+ux*6.0*g.sc, by+uy*6.0*g.sc, 5.4*g.sc, darkInk)
		coilTopX := bx + ux*(a.length*0.30) + pxN*(whipPx*0.09)
		coilTopY := by + uy*(a.length*0.30) + pyN*(whipPx*0.09)
		r.aaLine(coilTopX, coilTopY, tx, ty, 4.2*g.sc, darkInk)
		for ci := 0; ci < 7; ci++ {
			frac := 0.04 + float64(ci)*0.038
			ccx := bx + ux*(a.length*frac) + pxN*(whipPx*frac*frac)
			ccy := by + uy*(a.length*frac) + pyN*(whipPx*frac*frac)
			r.aaLine(ccx-pxN*6.4*g.sc, ccy-pyN*6.4*g.sc, ccx+pxN*6.4*g.sc, ccy+pyN*6.4*g.sc, 4.0*g.sc, darkInk)
			// Subtle metallic highlight across each spring coil turn
			r.aaLine(ccx-pxN*3.8*g.sc, ccy-pyN*3.8*g.sc-0.8*g.sc, ccx+pxN*3.8*g.sc, ccy+pyN*3.8*g.sc-0.8*g.sc, 1.3*g.sc, color.RGBA{92, 100, 118, 255})
		}
		r.aaLine(tipBX, tipBY, tx, ty, 8.6*g.sc, darkInk)
		r.aaLine(tipBX+pxN*1.3*g.sc, tipBY+2.8*g.sc, tx+pxN*1.3*g.sc, ty-2.8*g.sc, 2.0*g.sc, color.RGBA{96, 102, 122, 255})
	}
}

func paintOpticalLens(r *renderer, lcx, lcy, lr, blink float64, snap robot.Snapshot, g robotGeom, tSec float64, darkInk, pureWhite color.RGBA) {
	creamLid := color.RGBA{246, 239, 229, 255}
	warmBronze := color.RGBA{88, 60, 44, 255}

	// Outer dark ink spectacle rim + gunmetal/bronze inner bevel + deep obsidian glass
	r.aaDisc(lcx, lcy, lr+3.6*g.sc, darkInk)
	r.aaDisc(lcx, lcy, lr+1.0*g.sc, color.RGBA{46, 40, 48, 255})
	r.aaDisc(lcx-1.6*g.sc, lcy-1.6*g.sc, lr, warmBronze)
	r.aaDisc(lcx, lcy, lr-2.4*g.sc, color.RGBA{12, 10, 16, 255})

	// Gaze + micro-saccade tracking inside the optical lens barrel
	pX := snap.LookX*10.0*g.sc + g.talkEnergy*1.8*g.sc*math.Sin(tSec*9.5)
	pY := snap.LookY*7.5*g.sc + g.talkEnergy*1.5*g.sc*math.Cos(tSec*11.5)

	switch snap.Mood {
	case robot.MoodHappy:
		arcR := lr * 0.58
		r.aaRing(lcx+pX*0.4, lcy+arcR*0.25+pY*0.4, arcR, max(3.4, lr*0.18), math.Pi*1.18, math.Pi*1.82, pureWhite)
		return
	case robot.MoodSleepy:
		drawLensEyelid(r, lcx, lcy, lr-1.2*g.sc, 0.72, 0.0, creamLid, darkInk)
		return
	}

	// Voice-reactive internal camera aperture / iris ring that dilates & contracts with spoken syllables
	syllablePulse := g.talkEnergy*(0.60+0.40*math.Sin(tSec*14.5)) + g.listenLevel*0.55
	irisRad := lr * (0.42 + 0.14*syllablePulse)
	irisCol := lerp(color.RGBA{34, 30, 42, 255}, color.RGBA{128, 92, 68, 255}, clamp01(syllablePulse*1.1))
	r.aaRing(lcx+pX*0.55, lcy+pY*0.55, irisRad, max(1.8, lr*0.065), 0, 2*math.Pi, irisCol)

	// Lower-left warm amber/brown internal lens reflection crescent
	refCX := lcx - lr*0.10 + pX*0.45
	refCY := lcy + lr*0.10 + pY*0.45
	r.aaRing(refCX, refCY, lr*0.60, max(2.8, lr*0.15), math.Pi*0.48, math.Pi*0.92, warmBronze)

	// Upper-left subtle warm dome sheen inside glass
	drawAlphaDisc(r, lcx-lr*0.12+pX*0.55, lcy-lr*0.18+pY*0.55, lr*0.32, color.RGBA{58, 40, 32, 150})

	scale := 1.0 + 0.14*syllablePulse
	if snap.Mood == robot.MoodStartled {
		scale = 1.25
	} else if snap.Mood == robot.MoodCurious || snap.Mood == robot.MoodListening {
		scale = max(scale, 1.12)
	}

	// Primary upper-right glossy white bean specular catchlight
	hx := lcx + lr*0.22 + pX
	hy := lcy - lr*0.18 + pY
	br := lr * 0.20 * scale
	r.aaLine(hx-br*0.28, hy-br*0.28, hx+br*0.22, hy+br*0.28, br*1.65, pureWhite)

	// Secondary lower-right crisp white dot catchlight
	dx := lcx + lr*0.35 + pX*0.85
	dy := lcy + lr*0.24 + pY*0.85
	r.aaDisc(dx, dy, max(2.5, lr*0.10*scale), pureWhite)

	if blink > 0.04 || g.lowerSmile > 0.04 {
		drawLensEyelid(r, lcx, lcy, lr-1.2*g.sc, blink, g.lowerSmile, creamLid, darkInk)
	}
}

func drawLensEyelid(r *renderer, cx, cy, rad, topClosure, botSmile float64, lidCol, lashCol color.RGBA) {
	topClosure = clamp01(topClosure)
	botSmile = clamp01(botSmile)
	lidBottomY := cy - rad + 2.0*rad*topClosure
	lidTopFromBotY := cy + rad - 0.85*rad*botSmile
	r2 := rad * rad

	if topClosure > 0.04 {
		for y := int(math.Floor(cy - rad)); y <= int(math.Ceil(lidBottomY)); y++ {
			dy := float64(y) + 0.5 - cy
			if dy*dy >= r2 {
				continue
			}
			halfW := math.Sqrt(r2 - dy*dy)
			x0 := int(math.Floor(cx - halfW))
			x1 := int(math.Ceil(cx + halfW))
			col := lidCol
			if float64(y) >= lidBottomY-3.2 {
				col = lashCol
			}
			for x := x0; x <= x1; x++ {
				r.blendAt(x, y, col, 1.0)
			}
		}
	}

	if botSmile > 0.04 {
		shutterCol := color.RGBA{26, 22, 32, 255}
		rimCol := color.RGBA{88, 60, 44, 255}
		for y := int(math.Floor(lidTopFromBotY)); y <= int(math.Ceil(cy+rad)); y++ {
			dy := float64(y) + 0.5 - cy
			if dy*dy >= r2 {
				continue
			}
			halfW := math.Sqrt(r2 - dy*dy)
			x0 := int(math.Floor(cx - halfW))
			x1 := int(math.Ceil(cx + halfW))
			col := shutterCol
			if float64(y) <= lidTopFromBotY+2.4 {
				col = rimCol
			}
			for x := x0; x <= x1; x++ {
				r.blendAt(x, y, col, 1.0)
			}
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
