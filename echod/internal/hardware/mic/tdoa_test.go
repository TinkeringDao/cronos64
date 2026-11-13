package mic

import (
	"math"
	"math/rand"
	"testing"
)

// synthesizeSpeechBand generates a deterministic pulse train composed of speech-band harmonics (300-3400 Hz)
// at 16000 Hz.
func synthesizeSpeechBand(length int) []int16 {
	out := make([]int16, length)
	sampleRate := 16000.0
	// Harmonics of 150 Hz between 300 Hz and 3400 Hz
	harmonics := []float64{
		300, 450, 600, 750, 900, 1050, 1200, 1350, 1500,
		1650, 1800, 1950, 2100, 2250, 2400, 2550, 2700, 2850, 3000, 3150, 3300,
	}

	for i := range out {
		t := float64(i) / sampleRate
		var v float64
		for _, freq := range harmonics {
			v += math.Sin(2.0 * math.Pi * freq * t)
		}
		// Scale to ~ -18 dBFS (RMS ~ 4000)
		val := v * 400.0
		if val > 32000 {
			val = 32000
		} else if val < -32000 {
			val = -32000
		}
		out[i] = int16(val)
	}
	return out
}

func TestEstimateAzimuthSyntheticDelays(t *testing.T) {
	const sampleRate = 16000
	const micSpacingMeters = 0.055
	const frameLen = 320
	const bufferLen = 1000

	baseSignal := synthesizeSpeechBand(bufferLen)

	// Deterministic PRNG for decorrelated noise at ~ -42 dBFS (amplitude ~ 260)
	rng := rand.New(rand.NewSource(42))
	noiseLevel := 260.0

	// Negative shift = sound from Left (arrives at ch0/left first, ch1 delayed)
	// Positive shift = sound from Right (arrives at ch1/right first, ch1 leading)
	shifts := []int{-2, -1, 0, 1, 2}
	azimuths := make([]float64, len(shifts))

	for i, shift := range shifts {
		ch0 := make([]int16, frameLen)
		ch1 := make([]int16, frameLen)

		baseOffset := 200
		for j := 0; j < frameLen; j++ {
			// ch0 (Left mic) reference
			n0 := (rng.Float64()*2 - 1) * noiseLevel
			v0 := float64(baseSignal[baseOffset+j]) + n0
			ch0[j] = int16(math.Round(v0))

			// ch1 (Right mic):
			// shift < 0: sound hits ch0 first, ch1 receives it delayed by |shift| -> baseSignal[baseOffset+j+shift]
			// shift > 0: sound hits ch1 first, ch1 leads by shift -> baseSignal[baseOffset+j+shift]
			n1 := (rng.Float64()*2 - 1) * noiseLevel
			v1 := float64(baseSignal[baseOffset+j+shift]) + n1
			ch1[j] = int16(math.Round(v1))
		}

		az, conf, ok := EstimateAzimuth(ch0, ch1, sampleRate, micSpacingMeters)
		if !ok {
			t.Fatalf("shift %d: EstimateAzimuth returned ok=false", shift)
		}
		if conf < 0.6 {
			t.Fatalf("shift %d: confidence = %f, want > 0.6", shift, conf)
		}
		if az < -1.0 || az > 1.0 {
			t.Fatalf("shift %d: azimuth = %f, want in [-1.0, +1.0]", shift, az)
		}
		if shift < 0 && az >= 0 {
			t.Fatalf("shift %d (left source): azimuth = %f, want < 0", shift, az)
		}
		if shift > 0 && az <= 0 {
			t.Fatalf("shift %d (right source): azimuth = %f, want > 0", shift, az)
		}
		azimuths[i] = az
	}

	// Assert strict left-to-right monotonicity: azimuth(-2) < azimuth(-1) < azimuth(0) < azimuth(+1) < azimuth(+2)
	for i := 1; i < len(azimuths); i++ {
		if azimuths[i] <= azimuths[i-1] {
			t.Fatalf("azimuth not monotonic: shift %d (%f) <= shift %d (%f)",
				shifts[i], azimuths[i], shifts[i-1], azimuths[i-1])
		}
	}
}

func TestEstimateAzimuthRejectsSilenceClippingAndIdenticalMono(t *testing.T) {
	const sampleRate = 16000
	const micSpacingMeters = 0.055
	const frameLen = 320

	t.Run("SilenceAllZeros", func(t *testing.T) {
		ch0 := make([]int16, frameLen)
		ch1 := make([]int16, frameLen)
		_, _, ok := EstimateAzimuth(ch0, ch1, sampleRate, micSpacingMeters)
		if ok {
			t.Fatalf("expected ok=false on all-zeros silence")
		}
	})

	t.Run("LowFloorBelow55dBFS", func(t *testing.T) {
		// Amplitude 10 corresponds to ~ -70 dBFS (RMS < 58.0)
		ch0 := make([]int16, frameLen)
		ch1 := make([]int16, frameLen)
		for i := 0; i < frameLen; i++ {
			ch0[i] = int16((i % 5) * 2)
			ch1[i] = int16(((i + 2) % 5) * 2)
		}
		_, _, ok := EstimateAzimuth(ch0, ch1, sampleRate, micSpacingMeters)
		if ok {
			t.Fatalf("expected ok=false on low noise floor (RMS < -55 dBFS)")
		}
	})

	t.Run("ClippedSamples", func(t *testing.T) {
		baseSignal := synthesizeSpeechBand(frameLen)
		ch0 := make([]int16, frameLen)
		ch1 := make([]int16, frameLen)
		copy(ch0, baseSignal)
		copy(ch1, baseSignal)
		// Add small difference so not identical
		ch1[0] ^= 1
		// Add clipping sample >= 32760
		ch0[50] = 32767
		_, _, ok := EstimateAzimuth(ch0, ch1, sampleRate, micSpacingMeters)
		if ok {
			t.Fatalf("expected ok=false on clipped sample (32767)")
		}

		ch0[50] = 1000
		ch1[50] = -32768
		_, _, ok = EstimateAzimuth(ch0, ch1, sampleRate, micSpacingMeters)
		if ok {
			t.Fatalf("expected ok=false on clipped negative sample (-32768)")
		}
	})

	t.Run("ExactMonoDownmixed", func(t *testing.T) {
		baseSignal := synthesizeSpeechBand(frameLen)
		ch0 := make([]int16, frameLen)
		ch1 := make([]int16, frameLen)
		copy(ch0, baseSignal)
		copy(ch1, baseSignal) // Byte-identical channels!
		_, _, ok := EstimateAzimuth(ch0, ch1, sampleRate, micSpacingMeters)
		if ok {
			t.Fatalf("expected ok=false on exact mono-downmixed ch0 == ch1")
		}
	})
}

func encodeRawFrame(ch0, ch1 []int16) []byte {
	n := len(ch0)
	frameBytes := Channels * Bits / 8
	raw := make([]byte, n*frameBytes)
	for i := 0; i < n; i++ {
		off := i * frameBytes
		if Mics >= 1 {
			putS24LE3(raw[off:], int32(ch0[i])<<8)
		}
		if Mics >= 2 {
			putS24LE3(raw[off+3:], int32(ch1[i])<<8)
		}
	}
	return raw
}

func TestMicSourceAzimuthLazyDemand(t *testing.T) {
	s := testSource()

	// Initial call without prior demand or frame: ok should be false
	az, conf, ok := s.Azimuth()
	if ok {
		t.Fatalf("expected ok=false on first Azimuth call before any frames")
	}
	_ = az
	_ = conf

	// Prepare synthetic frame
	const frameLen = FrameSamples
	baseSignal := synthesizeSpeechBand(frameLen + 10)
	ch0 := make([]int16, frameLen)
	ch1 := make([]int16, frameLen)
	for i := 0; i < frameLen; i++ {
		ch0[i] = baseSignal[i+4]
		// Sound arriving from the right (ch1 leading by +2 samples)
		ch1[i] = baseSignal[i+4+2] + int16((i%3)-1)
	}

	raw := encodeRawFrame(ch0, ch1)

	// Since Azimuth() was called above, wantAzimuth is set.
	// broadcast will evaluate EstimateAzimuth if Mics == 2.
	s.broadcast(raw)

	az, conf, ok = s.Azimuth()
	if Mics != 2 {
		// Non-2-mic devices (Dot 7-mic, Spot/Donut 4-mic, Panel 1-mic) must leave Azimuth() inactive (ok == false)
		if ok {
			t.Fatalf("expected ok=false on %d-mic device (build tags), got ok=true", Mics)
		}
		return
	}

	// 2-mic Cronos device: azimuth must be active with positive azimuth for right-side sound arrival
	if !ok {
		t.Fatalf("expected ok=true after broadcast with pending wantAzimuth on 2-mic device")
	}
	if conf < 0.6 {
		t.Errorf("conf = %f, want > 0.6", conf)
	}
	if az <= 0 {
		t.Errorf("azimuth = %f, want > 0 for right-side sound arrival (ch1 leading)", az)
	}
}
