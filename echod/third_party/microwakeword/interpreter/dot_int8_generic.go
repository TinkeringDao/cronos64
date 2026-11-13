//go:build !arm64 || noasm

package interpreter

// dotInt8 is the sum of (act[i] + actOffset) * weights[i] over act, in int32 (wrapping); weights is at
// least as long as act. Four sums, so consecutive multiplies do not wait on one another; int32 addition
// wraps the same in any order, so the result is exact.
func dotInt8(act, weights []int8, actOffset int32) int32 {
	w := weights[:len(act)]
	var a0, a1, a2, a3 int32
	i := 0
	for ; i+4 <= len(act); i += 4 {
		a0 += (int32(act[i]) + actOffset) * int32(w[i])
		a1 += (int32(act[i+1]) + actOffset) * int32(w[i+1])
		a2 += (int32(act[i+2]) + actOffset) * int32(w[i+2])
		a3 += (int32(act[i+3]) + actOffset) * int32(w[i+3])
	}
	for ; i < len(act); i++ {
		a0 += (int32(act[i]) + actOffset) * int32(w[i])
	}
	return a0 + a1 + a2 + a3
}
