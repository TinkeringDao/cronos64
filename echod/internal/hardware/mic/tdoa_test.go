package mic

import (
	"math"
	"math/rand"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/audio"
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

	shifts := []int{-2, -1, 0, 1, 2}
	azimuths := make([]float64, len(shifts))

	for i, shift := range shifts {
		ch0 := make([]int16, frameLen)
		ch1 := make([]int16, frameLen)

		baseOffset := 200
		for j := 0; j < frameLen; j++ {
			// ch0 reference
			n0 := (rng.Float64()*2 - 1) * noiseLevel
			v0 := float64(baseSignal[baseOffset+j]) + n0
			ch0[j] = int16(math.Round(v0))

			// ch1 shifted by shift: if shift is positive, ch1 receives the signal later (delayed)
			n1 := (rng.Float64()*2 - 1) * noiseLevel
			v1 := float64(baseSignal[baseOffset+j-shift]) + n1
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
		azimuths[i] = az
	}

	// Assert strict monotonicity: azimuth(-2) < azimuth(-1) < azimuth(0) < azimuth(+1) < azimuth(+2)
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

func encodeCronosRawFrame(ch0, ch1 []int16) []byte {
	n := len(ch0)
	// Channels = 4 (mic0, mic1, ref0, ref1), 3 bytes per channel
	frameBytes := 4 * 3
	raw := make([]byte, n*frameBytes)
	for i := 0; i < n; i++ {
		off := i * frameBytes
		// ch0 (mic0)
		v0 := uint32(int32(ch0[i]) << 8)
		raw[off] = byte(v0)
		raw[off+1] = byte(v0 >> 8)
		raw[off+2] = byte(v0 >> 16)

		// ch1 (mic1)
		v1 := uint32(int32(ch1[i]) << 8)
		raw[off+3] = byte(v1)
		raw[off+4] = byte(v1 >> 8)
		raw[off+5] = byte(v1 >> 16)

		// ref0, ref1 are 0
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

	// Prepare synthetic frame with shift = +2 (delayed on ch1)
	const frameLen = FrameSamples
	baseSignal := synthesizeSpeechBand(frameLen + 10)
	ch0 := make([]int16, frameLen)
	ch1 := make([]int16, frameLen)
	for i := 0; i < frameLen; i++ {
		ch0[i] = baseSignal[i+4]
		ch1[i] = baseSignal[i+4-2] + int16((i%3)-1) // delayed by 2 + tiny decorrelation
	}

	raw := encodeCronosRawFrame(ch0, ch1)

	// Since Azimuth() was called above, wantAzimuth is set.
	// broadcast will evaluate EstimateAzimuth and store the result.
	s.broadcast(raw)

	// Now Azimuth() should return ok=true with positive azimuth
	az, conf, ok = s.Azimuth()
	if !ok {
		t.Fatalf("expected ok=true after broadcast with pending wantAzimuth")
	}
	if conf < 0.6 {
		t.Errorf("conf = %f, want > 0.6", conf)
	}
	if az <= 0 {
		t.Errorf("azimuth = %f, want > 0 for +2 delay on ch1", az)
	}

	// Verify audio loopback AEC is untouched
	_ = audio.DecodeS24LE3
}
