package interpreter

import (
	"math/rand"
	"testing"
)

// dotInt8Ref is the definition: the sum of (act[i] + actOffset) * weights[i] in int32, wrapping as
// int32 arithmetic does. weights is at least as long as act.
func dotInt8Ref(act, weights []int8, actOffset int32) int32 {
	var acc int32
	for i, a := range act {
		acc += (int32(a) + actOffset) * int32(weights[i])
	}
	return acc
}

var dotLengths = []int{0, 1, 7, 8, 15, 16, 17, 31, 32, 33, 63, 64, 65, 127, 128, 129, 255, 256, 257, 1000, 4096}

// dotInt8 (NEON on arm64, the loop elsewhere) gives the reference's int32 exactly: every length round
// the 16-lane blocks and their tails, the extremes of int8 (-128*-128 is the one product that does not
// fit a doubled int16 lane), random data, and every activation offset a zero point makes.
func TestDotInt8MatchesReferenceAcrossLengths(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	fills := map[string]func(i int) (int8, int8){
		"min*min": func(int) (int8, int8) { return -128, -128 },
		"max*max": func(int) (int8, int8) { return 127, 127 },
		"min*max": func(int) (int8, int8) { return -128, 127 },
		"alt":     func(i int) (int8, int8) { return int8(-128 + 255*(i&1)), int8(127 - 255*(i>>1&1)) },
		"random":  func(int) (int8, int8) { return int8(r.Intn(256) - 128), int8(r.Intn(256) - 128) },
	}
	for name, fill := range fills {
		for _, n := range dotLengths {
			// One spare element past the end of each slice, set to something that would show if it
			// were read.
			a, w := make([]int8, n+1), make([]int8, n+1)
			for i := 0; i < n; i++ {
				a[i], w[i] = fill(i)
			}
			a[n], w[n] = 99, 99
			for _, off := range []int32{-128, -127, -1, 0, 1, 5, 127, 128} {
				got, want := dotInt8(a[:n], w, off), dotInt8Ref(a[:n], w, off)
				if got != want {
					t.Fatalf("%s n=%d off=%d: dotInt8 = %d, want %d", name, n, off, got, want)
				}
			}
		}
	}
}

// Every offset from -128 to 128 (a zero point's whole range, negated) on random data, at unaligned
// starts within the slices.
func TestDotInt8EveryOffsetUnaligned(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	buf := make([]int8, 600)
	wbuf := make([]int8, 600)
	for i := range buf {
		buf[i], wbuf[i] = int8(r.Intn(256)-128), int8(r.Intn(256)-128)
	}
	for off := int32(-128); off <= 128; off++ {
		s := int(off+128) % 13
		n := 1 + int(off+128)*2
		got, want := dotInt8(buf[s:s+n], wbuf[s+1:], off), dotInt8Ref(buf[s:s+n], wbuf[s+1:], off)
		if got != want {
			t.Fatalf("off=%d n=%d start=%d: dotInt8 = %d, want %d", off, n, s, got, want)
		}
	}
}

// Sums that overflow int32 wrap as the reference's do.
func TestDotInt8WrapsLikeInt32(t *testing.T) {
	n := 1 << 17 // 131072 * 128 * 255 > 2^31
	a, w := make([]int8, n), make([]int8, n)
	for i := range a {
		a[i], w[i] = 127, -128
	}
	for _, off := range []int32{128, -128} {
		if got, want := dotInt8(a, w, off), dotInt8Ref(a, w, off); got != want {
			t.Fatalf("off=%d: dotInt8 = %d, want %d", off, got, want)
		}
	}
}

var dotSink int32

func benchDot(b *testing.B, n int, f func([]int8, []int8, int32) int32) {
	r := rand.New(rand.NewSource(5))
	a, w := make([]int8, n), make([]int8, n)
	for i := range a {
		a[i], w[i] = int8(r.Intn(256)-128), int8(r.Intn(256)-128)
	}
	b.SetBytes(int64(n))
	b.ResetTimer()
	for range b.N {
		dotSink += f(a, w, -3)
	}
}

func BenchmarkDotInt8_64(b *testing.B)     { benchDot(b, 64, dotInt8) }
func BenchmarkDotInt8Ref_64(b *testing.B)  { benchDot(b, 64, dotInt8Ref) }
func BenchmarkDotInt8_640(b *testing.B)    { benchDot(b, 640, dotInt8) }
func BenchmarkDotInt8Ref_640(b *testing.B) { benchDot(b, 640, dotInt8Ref) }
func BenchmarkDotInt8_4096(b *testing.B)   { benchDot(b, 4096, dotInt8) }
func BenchmarkDotInt8Ref_4096(b *testing.B) {
	benchDot(b, 4096, dotInt8Ref)
}
