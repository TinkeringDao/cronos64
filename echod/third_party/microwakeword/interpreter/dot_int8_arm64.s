//go:build arm64 && !noasm

#include "textflag.h"

// func dotInt8NEON(act, weights *int8, n int, actOffset int32) int32
//
// sum((act[i] + actOffset) * weights[i]) for i < n, in int32 arithmetic (wrapping), ARMv8.0 NEON (the
// Cortex-A53 has no SDOT). The offset is taken out of the loop: the sum is sum(act*w) + actOffset*sum(w),
// equal modulo 2^32, which is what int32 wraps to, so bit for bit the reference's. act*w fits an int16
// lane (|-128*-128| = 16384) but a pair of them does not (2*16384 = 32768), so each product vector is
// widened into int32 lanes on its own: SMULL/SMULL2 (int8 x int8 -> int16), SADALP (pairwise add into
// int32). The weights' sum is SADDLP (int8 pairs -> int16) then SADALP.
//
// 32 bytes an iteration on four independent product chains, then one of 16, then the scalar tail. Go's
// assembler has no SMULL/SMULL2/SADDLP/SADALP mnemonics, so they are WORDs, each with the instruction
// it encodes beside it (checked against clang's assembler).
//
// V16, V17, V20, V21: int32 product sums; V18, V19: int32 weight sums; R6: the tail's int32 sum.
TEXT ·dotInt8NEON(SB), NOSPLIT, $0-36
	MOVD	act+0(FP), R0
	MOVD	weights+8(FP), R1
	MOVD	n+16(FP), R2
	MOVW	actOffset+24(FP), R3

	VEOR	V16.B16, V16.B16, V16.B16
	VEOR	V17.B16, V17.B16, V17.B16
	VEOR	V18.B16, V18.B16, V18.B16
	VEOR	V19.B16, V19.B16, V19.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	MOVW	$0, R6

loop32:
	CMP	$32, R2
	BLT	loop16
	VLD1.P	32(R0), [V0.B16, V1.B16]
	VLD1.P	32(R1), [V2.B16, V3.B16]
	WORD	$0x0e22c004 // SMULL  V4.8H, V0.8B, V2.8B
	WORD	$0x4e22c005 // SMULL2 V5.8H, V0.16B, V2.16B
	WORD	$0x0e23c026 // SMULL  V6.8H, V1.8B, V3.8B
	WORD	$0x4e23c027 // SMULL2 V7.8H, V1.16B, V3.16B
	WORD	$0x4e202858 // SADDLP V24.8H, V2.16B
	WORD	$0x4e202879 // SADDLP V25.8H, V3.16B
	WORD	$0x4e606890 // SADALP V16.4S, V4.8H
	WORD	$0x4e6068b1 // SADALP V17.4S, V5.8H
	WORD	$0x4e6068d4 // SADALP V20.4S, V6.8H
	WORD	$0x4e6068f5 // SADALP V21.4S, V7.8H
	WORD	$0x4e606b12 // SADALP V18.4S, V24.8H
	WORD	$0x4e606b33 // SADALP V19.4S, V25.8H
	SUB	$32, R2
	B	loop32

loop16:
	CMP	$16, R2
	BLT	tail
	VLD1.P	16(R0), [V0.B16]
	VLD1.P	16(R1), [V2.B16]
	WORD	$0x0e22c004 // SMULL  V4.8H, V0.8B, V2.8B
	WORD	$0x4e22c005 // SMULL2 V5.8H, V0.16B, V2.16B
	WORD	$0x4e202858 // SADDLP V24.8H, V2.16B
	WORD	$0x4e606890 // SADALP V16.4S, V4.8H
	WORD	$0x4e6068b1 // SADALP V17.4S, V5.8H
	WORD	$0x4e606b12 // SADALP V18.4S, V24.8H
	SUB	$16, R2

tail:
	CBZ	R2, reduce
	MOVB.P	1(R0), R4 // sign-extending
	MOVB.P	1(R1), R5
	ADDW	R3, R4, R4
	MADDW	R4, R6, R5, R6 // R6 += R5 * R4
	SUB	$1, R2
	B	tail

reduce:
	VADD	V17.S4, V16.S4, V16.S4
	VADD	V21.S4, V20.S4, V20.S4
	VADD	V20.S4, V16.S4, V16.S4
	VADD	V19.S4, V18.S4, V18.S4
	VADDV	V16.S4, V16
	VADDV	V18.S4, V18
	VMOV	V16.S[0], R7
	VMOV	V18.S[0], R8
	MADDW	R3, R7, R8, R7 // R7 += sum(w) * actOffset
	ADDW	R6, R7, R7
	MOVW	R7, ret+32(FP)
	RET
