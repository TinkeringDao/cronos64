package memx

import (
	"runtime"
	"testing"
)

// The assembly is ARMv7's (PLD and MOVM, for cores that do not prefetch on their own). A 64-bit
// build takes the Go paths, whose copy is the runtime's memmove: on arm64 that is already a
// 16-byte-a-load NEON/LDP loop with the cores' own prefetch behind it, so there is nothing for
// assembly here to win. This pins the choice; copy_test.go and sum_test.go check the results agree
// with the Go reference on whichever path is taken.
func TestAssemblyOnlyOnARMv7(t *testing.T) {
	if runtime.GOARCH != "arm" && asm {
		t.Fatalf("GOARCH=%s takes the ARMv7 assembly", runtime.GOARCH)
	}
}
