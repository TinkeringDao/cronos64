//go:build arm64 && !noasm

package interpreter

// dotInt8 is the sum of (act[i] + actOffset) * weights[i] over act, in int32 (wrapping); weights is at
// least as long as act. On arm64 it is NEON, 32 lanes an iteration (dot_int8_arm64.s); elsewhere the
// loop in dot_int8_generic.go. dot_int8_test.go holds both to the reference.
func dotInt8(act, weights []int8, actOffset int32) int32 {
	n := len(act)
	if n == 0 {
		return 0
	}
	_ = weights[n-1] // the assembly reads n weights
	return dotInt8NEON(&act[0], &weights[0], n, actOffset)
}

//go:noescape
func dotInt8NEON(act, weights *int8, n int, actOffset int32) int32
