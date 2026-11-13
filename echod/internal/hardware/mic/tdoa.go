package mic

import (
	"math"
)

// EstimateAzimuth calculates the sound arrival azimuth across a 2-microphone array using
// mean-removed normalized cross-correlation (NCC) with 3-point parabolic sub-sample interpolation.
//
// Returns:
// - azimuth: normalized direction in [-1.0, +1.0] (left-to-right)
// - confidence: peak normalized cross-correlation in [0.0, 1.0]
// - ok: true if the estimate is valid, false if rejected due to silence, clipping, or mono-downmix
func EstimateAzimuth(ch0, ch1 []int16, sampleRate int, micSpacingMeters float64) (azimuth float64, confidence float64, ok bool) {
	n := len(ch0)
	if n == 0 || n != len(ch1) || sampleRate <= 0 || micSpacingMeters <= 0 {
		return 0, 0, false
	}

	var sumSq0, sumSq1, diffSq float64
	var clipped bool
	for i := 0; i < n; i++ {
		s0 := ch0[i]
		s1 := ch1[i]

		if s0 <= -32760 || s0 >= 32760 || s1 <= -32760 || s1 >= 32760 {
			clipped = true
		}

		f0 := float64(s0)
		f1 := float64(s1)
		sumSq0 += f0 * f0
		sumSq1 += f1 * f1

		d := f0 - f1
		diffSq += d * d
	}

	// Reject exact mono-downmixed audio (ch0 == ch1 across all samples)
	if diffSq == 0 {
		return 0, 0, false
	}

	// Reject clipping (peak >= 32760)
	if clipped {
		return 0, 0, false
	}

	// Reject low floor / silence (RMS < 58.0, corresponding to ~ -55 dBFS)
	fn := float64(n)
	rms0 := math.Sqrt(sumSq0 / fn)
	rms1 := math.Sqrt(sumSq1 / fn)
	if rms0 < 58.0 || rms1 < 58.0 {
		return 0, 0, false
	}

	maxTau := (micSpacingMeters / 343.0) * float64(sampleRate)
	maxLag := int(math.Ceil(maxTau))
	searchL := maxLag + 1

	if n <= 2*searchL {
		return 0, 0, false
	}

	start := searchL
	end := n - searchL
	m := float64(end - start)

	var mean0 float64
	for i := start; i < end; i++ {
		mean0 += float64(ch0[i])
	}
	mean0 /= m

	var var0 float64
	for i := start; i < end; i++ {
		d0 := float64(ch0[i]) - mean0
		var0 += d0 * d0
	}
	if var0 == 0 {
		return 0, 0, false
	}

	numLags := 2*searchL + 1
	ncc := make([]float64, numLags)
	bestLagIdx := -1
	bestVal := -math.MaxFloat64

	for lag := -searchL; lag <= searchL; lag++ {
		idx := lag + searchL

		var mean1 float64
		for i := start; i < end; i++ {
			mean1 += float64(ch1[i+lag])
		}
		mean1 /= m

		var var1, cov float64
		for i := start; i < end; i++ {
			d0 := float64(ch0[i]) - mean0
			d1 := float64(ch1[i+lag]) - mean1
			cov += d0 * d1
			var1 += d1 * d1
		}

		if var1 == 0 {
			ncc[idx] = 0
			continue
		}

		r := cov / math.Sqrt(var0*var1)
		ncc[idx] = r
		if r > bestVal {
			bestVal = r
			bestLagIdx = idx
		}
	}

	if bestLagIdx < 0 || bestVal <= 0 {
		return 0, 0, false
	}

	bestLag := bestLagIdx - searchL
	tau := float64(bestLag)

	// 3-point parabolic sub-sample interpolation around peak lag
	if bestLagIdx > 0 && bestLagIdx < numLags-1 {
		yMinus := ncc[bestLagIdx-1]
		y0 := ncc[bestLagIdx]
		yPlus := ncc[bestLagIdx+1]

		denom := 2.0 * (yMinus - 2.0*y0 + yPlus)
		if math.Abs(denom) > 1e-9 {
			delta := (yMinus - yPlus) / denom
			if delta >= -0.5 && delta <= 0.5 {
				tau += delta
			}
		}
	}

	az := tau / maxTau
	if az < -1.0 {
		az = -1.0
	} else if az > 1.0 {
		az = 1.0
	}

	conf := bestVal
	if conf < 0 {
		conf = 0
	} else if conf > 1.0 {
		conf = 1.0
	}

	return az, conf, true
}
