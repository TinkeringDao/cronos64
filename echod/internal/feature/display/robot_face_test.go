//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/robot"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

func testRobotScene(d *Display, phase string, now time.Time) scene {
	s := scene{now: now, phase: phase, heard: "hey", reply: "hello", since: now}
	rawPhase, rawHeard, rawReply := d.applyTurnCue(&s, false, now)
	d.customScene(&s, now)
	d.finishCustomTurn(&s, rawPhase, rawHeard, rawReply, now)
	return s
}

func TestRobotFaceRetains60HzPacingAcrossVoicePhases(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	t.Cleanup(func() {
		_ = config.Set().Screen().IdleScreen("")
		_ = config.Set().Screen().TurnStyle("")
		robot.Get().SetAsleep(false)
	})
	if err := config.Set().Screen().IdleScreen("robot"); err != nil {
		t.Fatalf("IdleScreen(robot) failed: %v", err)
	}

	c, ok := customByID("robot")
	if !ok {
		t.Fatal("robot custom screen is not registered")
	}
	if !c.keeps || !c.follow || !c.duringTurn {
		t.Fatalf("robot screen flags = {keeps:%v follow:%v duringTurn:%v}, want all true", c.keeps, c.follow, c.duringTurn)
	}

	d := &Display{
		r:       newRenderer(image.NewRGBA(image.Rect(0, 0, 960, 480))),
		poke:    make(chan struct{}, 1),
		on:      true,
		ceiling: 80,
		level:   80,
	}

	styles := []string{"", "wave", "face", "cue", "captions"}
	if hasRainbow {
		styles = append(styles, "rainbow")
	}
	phases := []string{"idle", "listening", "thinking", "replying", "lingering"}
	now := time.Now().Add(time.Minute)

	for _, style := range styles {
		if err := config.Set().Screen().TurnStyle(style); err != nil {
			t.Fatalf("TurnStyle(%q): %v", style, err)
		}
		for _, phase := range phases {
			now = now.Add(20 * time.Millisecond)
			d.mu.Lock()
			d.view.Phase, d.view.Heard, d.view.Reply = phase, "hey", "hello"
			d.viewAt = now
			d.mu.Unlock()

			s := testRobotScene(d, phase, now)
			if s.custom.name != "robot" {
				t.Fatalf("style=%q phase=%q: s.custom.name = %q, want robot", style, phase, s.custom.name)
			}
			if s.phase != phase {
				t.Fatalf("style=%q phase=%q: s.phase = %q, want %q", style, phase, s.phase, phase)
			}
			if turnShown(s) {
				t.Fatalf("style=%q phase=%q: turnShown(s) = true, want false when robot owns turn", style, phase)
			}
			if rainbowHere(s) {
				t.Fatalf("style=%q phase=%q: rainbowHere(s) = true, want false when robot is active", style, phase)
			}
			if !customAlonePage(s) {
				t.Fatalf("style=%q phase=%q: customAlonePage(s) = false, want true", style, phase)
			}
			if !d.customShowing() {
				t.Fatalf("style=%q phase=%q: customShowing() = false, want true", style, phase)
			}
			if next := d.frameNext(s, ringState{}, phone.State{}, now); next != 16*time.Millisecond {
				t.Fatalf("style=%q phase=%q: frameNext = %v, want 16ms", style, phase, next)
			}
		}
	}

	// Verify forehead swipe touch forwards to robot.Get() even via Display gesture routing.
	d.mu.Lock()
	d.view.Phase, d.view.Heard, d.view.Reply = "idle", "", ""
	d.viewAt = now
	d.mu.Unlock()
	_ = testRobotScene(d, "idle", now)
	d.gesture(touch.Gesture{Kind: touch.SwipeRight, X: 480, Y: 100})
	if snap := robot.Get().Snapshot(time.Now()); snap.Mood != robot.MoodHappy || snap.PetGlow <= 0 {
		t.Fatalf("after Display forehead swipe, robot snap = %+v, want MoodHappy + PetGlow > 0", snap)
	}
}

func TestRobotFaceDirtyRectClearsPreviousEyePositionsWithoutTrails(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	t.Cleanup(func() {
		_ = config.Set().Screen().IdleScreen("")
		_ = config.Set().Screen().TurnStyle("")
		robot.Get().SetAsleep(false)
	})
	if err := config.Set().Screen().IdleScreen("robot"); err != nil {
		t.Fatal(err)
	}

	// Choose a monotonic future timestamp cleanly past any other test's clock.
	t0 := time.Now().Add(10 * time.Minute).Truncate(time.Minute).Add(10 * time.Second)
	rb := robot.Get()
	rb.SetAsleep(false)
	rb.ObserveTurn("idle", 0, 0, t0)

	// Settle eyes strongly to the left (-0.9, -0.4).
	rb.ObserveGaze(robot.GazeSample{X: -0.9, Y: -0.4, Confidence: 0.95, At: t0})
	for i := 1; i <= 30; i++ {
		_ = rb.Snapshot(t0.Add(time.Duration(i) * 16 * time.Millisecond))
	}
	tLeft := t0.Add(30 * 16 * time.Millisecond)

	img := image.NewRGBA(image.Rect(0, 0, 960, 480))
	r := newRenderer(img)

	s1 := scene{
		now:   tLeft,
		phase: "idle",
		custom: customView{
			name: "robot",
		},
	}
	r.draw(s1)

	cache1 := customCache(r, "robot", func() *robotRenderCache { return &robotRenderCache{} })
	if cache1.dirty.Empty() {
		t.Fatal("frame 1 did not record a non-empty dirty rect")
	}

	// Capture every lit (non-walnut) pixel inside frame 1's face region before frame 2 moves right.
	var frame1Lit []image.Point
	for y := cache1.dirty.Min.Y; y < cache1.dirty.Max.Y; y++ {
		for x := cache1.dirty.Min.X; x < cache1.dirty.Max.X; x++ {
			if img.RGBAAt(x, y) != walnut {
				frame1Lit = append(frame1Lit, image.Pt(x, y))
			}
		}
	}
	if len(frame1Lit) < 100 {
		t.Fatalf("expected eye/mouth pixels drawn on frame 1, got %d", len(frame1Lit))
	}

	// Plant sentinel markers outside the face dirty box (top-left edge under bar & bottom-right corner)
	// to verify frame 2 only clears prevDirty.Union(curDirty) and leaves surrounding pixels untouched.
	sentinelColor := color.RGBA{12, 34, 56, 255}
	sentinels := []image.Point{
		image.Pt(12, r.s(customBarH)+8),
		image.Pt(945, 468),
	}
	for _, pt := range sentinels {
		if pt.In(cache1.dirty) {
			t.Fatalf("dirty rect %v unexpectedly covers margin sentinel %v", cache1.dirty, pt)
		}
		img.SetRGBA(pt.X, pt.Y, sentinelColor)
	}

	// Shift eyes strongly to the right (+0.9, +0.4) within the same minute.
	tRight := tLeft.Add(50 * time.Millisecond)
	rb.ObserveGaze(robot.GazeSample{X: 0.9, Y: 0.4, Confidence: 0.95, At: tRight})
	for i := 1; i <= 35; i++ {
		tRight = tRight.Add(16 * time.Millisecond)
		_ = rb.Snapshot(tRight)
	}

	s2 := scene{
		now:   tRight,
		phase: "idle",
		custom: customView{
			name: "robot",
		},
	}
	prevDirty := cache1.dirty
	r.draw(s2)

	if !r.customKept {
		t.Fatal("frame 2 did not preserve kept custom canvas (r.customKept == false)")
	}

	cache2 := customCache(r, "robot", func() *robotRenderCache { return &robotRenderCache{} })

	// Check sentinels outside prevDirty.Union(curDirty) remained completely untouched.
	unionDirty := prevDirty.Union(cache2.dirty)
	for _, pt := range sentinels {
		if pt.In(unionDirty) {
			t.Fatalf("union dirty rect %v unexpectedly covers margin sentinel %v", unionDirty, pt)
		}
		if got := img.RGBAAt(pt.X, pt.Y); got != sentinelColor {
			t.Fatalf("sentinel %v was overwritten (%v != %v): renderer cleared outside dirty union", pt, got, sentinelColor)
		}
	}

	// Verify old leftmost eye pixels from frame 1 (to the left of the rightmost eye edges moved away from)
	// were erased cleanly back to walnut without leaving ghost trails.
	erasedCount := 0
	for _, pt := range frame1Lit {
		if !pt.In(cache2.dirty) && img.RGBAAt(pt.X, pt.Y) != walnut {
			t.Fatalf("ghost pixel left behind at %v after frame 2: got %v, want %v", pt, img.RGBAAt(pt.X, pt.Y), walnut)
		}
		if img.RGBAAt(pt.X, pt.Y) == walnut {
			erasedCount++
		}
	}
	if erasedCount < 50 {
		t.Fatalf("expected >=50 old eye pixels to shift and clear to walnut, only %d cleared", erasedCount)
	}
}

func TestRobotFaceAllMoodsStayInsideDirtyRect(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().Add(20 * time.Minute)
	img := image.NewRGBA(image.Rect(0, 0, 960, 480))
	r := newRenderer(img)

	sentinelColor := color.RGBA{12, 34, 56, 255}
	sentinels := []image.Point{
		image.Pt(12, r.s(customBarH)+8),
		image.Pt(945, 468),
	}

	moods := []robot.Mood{
		robot.MoodCalm,
		robot.MoodCurious,
		robot.MoodHappy,
		robot.MoodListening,
		robot.MoodThinking,
		robot.MoodSpeaking,
		robot.MoodSleepy,
		robot.MoodStartled,
	}

	for i, m := range moods {
		snap := robot.Snapshot{
			LookX:           0.5,
			LookY:           -0.3,
			Mood:            m,
			MouthOpen:       0.7,
			ListenPulse:     0.8,
			PetGlow:         0.9,
			ThermalDegraded: i%2 == 1,
		}
		if i == 1 {
			for _, pt := range sentinels {
				img.SetRGBA(pt.X, pt.Y, sentinelColor)
			}
		}
		s := scene{
			now:   now.Add(time.Duration(i*50) * time.Millisecond),
			phase: "idle",
			custom: customView{
				name: robotScreenID,
				data: snap,
			},
		}
		r.draw(s)
		cache := customCache(r, robotScreenID, func() *robotRenderCache { return &robotRenderCache{} })
		if cache.dirty.Empty() {
			t.Fatalf("mood %q left empty dirty rect", m)
		}
		if i >= 1 {
			for _, pt := range sentinels {
				if pt.In(cache.dirty) {
					t.Fatalf("mood %q dirty rect %v covers sentinel %v", m, cache.dirty, pt)
				}
				if got := img.RGBAAt(pt.X, pt.Y); got != sentinelColor {
					t.Fatalf("mood %q overwrote margin sentinel %v: got %v want %v", m, pt, got, sentinelColor)
				}
			}
		}
	}
}
