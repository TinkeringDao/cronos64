package interpreter

// The original kernels, as published, for checking the optimized ones against.

import (
	"fmt"

	"github.com/zserge/microwakeword/model"
)

func refStridedSlice(interp *Interpreter, op model.Operator) error {
	if len(op.Inputs) < 4 || len(op.Outputs) != 1 {
		return fmt.Errorf("STRIDED_SLICE: bad io")
	}
	sg := interp.model.Subgraphs[interp.sgIdx]
	inShape := sg.Tensors[op.Inputs[0]].Shape
	inData := interp.tensors[op.Inputs[0]].data
	outShape := sg.Tensors[op.Outputs[0]].Shape
	out := &interp.tensors[op.Outputs[0]]

	ndim := len(inShape)
	beginData := interp.tensors[op.Inputs[1]].data
	endData := interp.tensors[op.Inputs[2]].data
	strideData := interp.tensors[op.Inputs[3]].data

	beginMask := 0
	endMask := 0
	if v, ok := op.Options["begin_mask"]; ok {
		beginMask = v.(int)
	}
	if v, ok := op.Options["end_mask"]; ok {
		endMask = v.(int)
	}

	begin := make([]int, ndim)
	end := make([]int, ndim)
	stride := make([]int, ndim)
	for d := 0; d < ndim; d++ {
		begin[d] = getInt32FromBytes(beginData, d)
		end[d] = getInt32FromBytes(endData, d)
		stride[d] = getInt32FromBytes(strideData, d)
		if beginMask&(1<<uint(d)) != 0 {
			begin[d] = 0
		}
		if endMask&(1<<uint(d)) != 0 {
			end[d] = inShape[d]
		}
		if begin[d] < 0 {
			begin[d] += inShape[d]
		}
		if end[d] < 0 {
			end[d] += inShape[d]
		}
		if stride[d] == 0 {
			stride[d] = 1
		}
	}

	// Iterate over output positions and copy from input
	outTotal := model.TensorSize(outShape)
	outCoord := make([]int, ndim)
	for i := 0; i < outTotal; i++ {
		// Compute input offset
		inOff := 0
		inStride := 1
		for d := ndim - 1; d >= 0; d-- {
			inIdx := begin[d] + outCoord[d]*stride[d]
			inOff += inIdx * inStride
			inStride *= inShape[d]
		}
		out.data[i] = inData[inOff]
		// Increment output coordinates
		for d := ndim - 1; d >= 0; d-- {
			outCoord[d]++
			if outCoord[d] < outShape[d] {
				break
			}
			outCoord[d] = 0
		}
	}
	return nil
}

// opConv2D implements quantized 2D convolution.
// Input: [batch, inH, inW, inC], Filter: [outC, fH, fW, inC], Bias: [outC]
// Output: [batch, outH, outW, outC]
func refConv2D(interp *Interpreter, op model.Operator) error {
	if len(op.Inputs) < 3 || len(op.Outputs) != 1 {
		return fmt.Errorf("CONV_2D: bad io")
	}
	sg := interp.model.Subgraphs[interp.sgIdx]

	inT := sg.Tensors[op.Inputs[0]]
	filterT := sg.Tensors[op.Inputs[1]]
	outT := sg.Tensors[op.Outputs[0]]

	inData := interp.tensors[op.Inputs[0]].data
	filterData := interp.tensors[op.Inputs[1]].data
	biasData := interp.tensors[op.Inputs[2]].data
	outData := interp.tensors[op.Outputs[0]].data

	padding := 0
	strideW := 1
	strideH := 1
	activation := 0
	dilationW := 1
	dilationH := 1
	if v, ok := op.Options["padding"]; ok {
		padding = v.(int)
	}
	if v, ok := op.Options["stride_w"]; ok {
		strideW = v.(int)
	}
	if v, ok := op.Options["stride_h"]; ok {
		strideH = v.(int)
	}
	if v, ok := op.Options["fused_activation"]; ok {
		activation = v.(int)
	}
	if v, ok := op.Options["dilation_w"]; ok {
		dilationW = v.(int)
	}
	if v, ok := op.Options["dilation_h"]; ok {
		dilationH = v.(int)
	}

	batch := inT.Shape[0]
	inH := inT.Shape[1]
	inW := inT.Shape[2]
	inC := inT.Shape[3]
	outC := filterT.Shape[0]
	fH := filterT.Shape[1]
	fW := filterT.Shape[2]
	outH := outT.Shape[1]
	outW := outT.Shape[2]

	inputZP := int32(0)
	if len(inT.Quant.ZeroPoint) > 0 {
		inputZP = int32(inT.Quant.ZeroPoint[0])
	}
	outputZP := int32(0)
	if len(outT.Quant.ZeroPoint) > 0 {
		outputZP = int32(outT.Quant.ZeroPoint[0])
	}

	// Compute per-channel multipliers
	inScale := float64(0)
	if len(inT.Quant.Scale) > 0 {
		inScale = float64(inT.Quant.Scale[0])
	}
	outScale := float64(0)
	if len(outT.Quant.Scale) > 0 {
		outScale = float64(outT.Quant.Scale[0])
	}
	qmuls := make([]QuantMultiplier, outC)
	for c := 0; c < outC; c++ {
		filterScale := float64(0)
		if c < len(filterT.Quant.Scale) {
			filterScale = float64(filterT.Quant.Scale[c])
		} else if len(filterT.Quant.Scale) > 0 {
			filterScale = float64(filterT.Quant.Scale[0])
		}
		if outScale == 0 {
			outScale = 1.0 // avoid division by zero
		}
		qmuls[c] = QuantizeMultiplier(inScale * filterScale / outScale)
	}

	// Compute padding offsets
	padH, padW := 0, 0
	if padding == model.PaddingSame {
		effFH := (fH-1)*dilationH + 1
		effFW := (fW-1)*dilationW + 1
		padH = ((outH-1)*strideH + effFH - inH) / 2
		padW = ((outW-1)*strideW + effFW - inW) / 2
		if padH < 0 {
			padH = 0
		}
		if padW < 0 {
			padW = 0
		}
	}

	// Activation bounds
	actMin, actMax := activationBounds(activation, outputZP)

	for b := 0; b < batch; b++ {
		for oh := 0; oh < outH; oh++ {
			for ow := 0; ow < outW; ow++ {
				for oc := 0; oc < outC; oc++ {
					acc := int32(getBias32(biasData, oc))
					for fh := 0; fh < fH; fh++ {
						for fw := 0; fw < fW; fw++ {
							ih := oh*strideH - padH + fh*dilationH
							iw := ow*strideW - padW + fw*dilationW
							if ih < 0 || ih >= inH || iw < 0 || iw >= inW {
								continue
							}
							for ic := 0; ic < inC; ic++ {
								inOff := ((b*inH+ih)*inW+iw)*inC + ic
								fOff := ((oc*fH+fh)*fW+fw)*inC + ic
								inVal := int32(int8(inData[inOff])) - inputZP
								fVal := int32(int8(filterData[fOff]))
								acc += inVal * fVal
							}
						}
					}
					acc = MultiplyByQuantizedMultiplier(acc, qmuls[oc])
					acc += outputZP
					if acc < actMin {
						acc = actMin
					}
					if acc > actMax {
						acc = actMax
					}
					outOff := ((b*outH+oh)*outW+ow)*outC + oc
					outData[outOff] = byte(int8(ClampInt8(acc)))
				}
			}
		}
	}
	return nil
}

// opDepthwiseConv2D implements quantized depthwise convolution.
// Input: [batch, inH, inW, inC], Filter: [1, fH, fW, outC], Bias: [outC]
// outC = inC * depth_multiplier
func refDepthwiseConv2D(interp *Interpreter, op model.Operator) error {
	if len(op.Inputs) < 3 || len(op.Outputs) != 1 {
		return fmt.Errorf("DEPTHWISE_CONV_2D: bad io")
	}
	sg := interp.model.Subgraphs[interp.sgIdx]

	inT := sg.Tensors[op.Inputs[0]]
	filterT := sg.Tensors[op.Inputs[1]]
	outT := sg.Tensors[op.Outputs[0]]

	inData := interp.tensors[op.Inputs[0]].data
	filterData := interp.tensors[op.Inputs[1]].data
	biasData := interp.tensors[op.Inputs[2]].data
	outData := interp.tensors[op.Outputs[0]].data

	padding := 0
	strideW := 1
	strideH := 1
	depthMul := 1
	activation := 0
	dilationW := 1
	dilationH := 1
	if v, ok := op.Options["padding"]; ok {
		padding = v.(int)
	}
	if v, ok := op.Options["stride_w"]; ok {
		strideW = v.(int)
	}
	if v, ok := op.Options["stride_h"]; ok {
		strideH = v.(int)
	}
	if v, ok := op.Options["depth_multiplier"]; ok {
		depthMul = v.(int)
	}
	if v, ok := op.Options["fused_activation"]; ok {
		activation = v.(int)
	}
	if v, ok := op.Options["dilation_w"]; ok {
		dilationW = v.(int)
	}
	if v, ok := op.Options["dilation_h"]; ok {
		dilationH = v.(int)
	}

	batch := inT.Shape[0]
	inH := inT.Shape[1]
	inW := inT.Shape[2]
	inC := inT.Shape[3]
	fH := filterT.Shape[1]
	fW := filterT.Shape[2]
	outC := inC * depthMul
	outH := outT.Shape[1]
	outW := outT.Shape[2]

	inputZP := int32(0)
	if len(inT.Quant.ZeroPoint) > 0 {
		inputZP = int32(inT.Quant.ZeroPoint[0])
	}
	outputZP := int32(0)
	if len(outT.Quant.ZeroPoint) > 0 {
		outputZP = int32(outT.Quant.ZeroPoint[0])
	}

	inScale := float64(0)
	if len(inT.Quant.Scale) > 0 {
		inScale = float64(inT.Quant.Scale[0])
	}
	outScale := float64(0)
	if len(outT.Quant.Scale) > 0 {
		outScale = float64(outT.Quant.Scale[0])
	}
	qmuls := make([]QuantMultiplier, outC)
	for c := 0; c < outC; c++ {
		filterScale := float64(0)
		if c < len(filterT.Quant.Scale) {
			filterScale = float64(filterT.Quant.Scale[c])
		} else if len(filterT.Quant.Scale) > 0 {
			filterScale = float64(filterT.Quant.Scale[0])
		}
		if outScale == 0 {
			outScale = 1.0
		}
		qmuls[c] = QuantizeMultiplier(inScale * filterScale / outScale)
	}

	padH, padW := 0, 0
	if padding == model.PaddingSame {
		effFH := (fH-1)*dilationH + 1
		effFW := (fW-1)*dilationW + 1
		padH = ((outH-1)*strideH + effFH - inH) / 2
		padW = ((outW-1)*strideW + effFW - inW) / 2
		if padH < 0 {
			padH = 0
		}
		if padW < 0 {
			padW = 0
		}
	}

	actMin, actMax := activationBounds(activation, outputZP)

	for b := 0; b < batch; b++ {
		for oh := 0; oh < outH; oh++ {
			for ow := 0; ow < outW; ow++ {
				for ic := 0; ic < inC; ic++ {
					for dm := 0; dm < depthMul; dm++ {
						oc := ic*depthMul + dm
						acc := int32(getBias32(biasData, oc))
						for fh := 0; fh < fH; fh++ {
							for fw := 0; fw < fW; fw++ {
								ih := oh*strideH - padH + fh*dilationH
								iw := ow*strideW - padW + fw*dilationW
								if ih < 0 || ih >= inH || iw < 0 || iw >= inW {
									continue
								}
								inOff := ((b*inH+ih)*inW+iw)*inC + ic
								fOff := (fh*fW+fw)*outC + oc
								inVal := int32(int8(inData[inOff])) - inputZP
								fVal := int32(int8(filterData[fOff]))
								acc += inVal * fVal
							}
						}
						acc = MultiplyByQuantizedMultiplier(acc, qmuls[oc])
						acc += outputZP
						if acc < actMin {
							acc = actMin
						}
						if acc > actMax {
							acc = actMax
						}
						outOff := ((b*outH+oh)*outW+ow)*outC + oc
						outData[outOff] = byte(int8(ClampInt8(acc)))
					}
				}
			}
		}
	}
	return nil
}

// refFullyConnected is opFullyConnected as published: the inner product one element at a time.
// Input: [batch, inputSize], Weights: [outputSize, inputSize], Bias: [outputSize]
func refFullyConnected(interp *Interpreter, op model.Operator) error {
	if len(op.Inputs) < 3 || len(op.Outputs) != 1 {
		return fmt.Errorf("FULLY_CONNECTED: bad io")
	}
	sg := interp.model.Subgraphs[interp.sgIdx]

	inT := sg.Tensors[op.Inputs[0]]
	weightT := sg.Tensors[op.Inputs[1]]
	outT := sg.Tensors[op.Outputs[0]]

	inData := interp.tensors[op.Inputs[0]].data
	weightData := interp.tensors[op.Inputs[1]].data
	biasData := interp.tensors[op.Inputs[2]].data
	outData := interp.tensors[op.Outputs[0]].data

	activation := 0
	if v, ok := op.Options["fused_activation"]; ok {
		activation = v.(int)
	}

	// For FC, input is flattened. Total input size from weights.
	outputSize := weightT.Shape[0]
	inputSize := weightT.Shape[1]
	batch := model.TensorSize(inT.Shape) / inputSize

	inputZP := int32(0)
	if len(inT.Quant.ZeroPoint) > 0 {
		inputZP = int32(inT.Quant.ZeroPoint[0])
	}
	outputZP := int32(0)
	if len(outT.Quant.ZeroPoint) > 0 {
		outputZP = int32(outT.Quant.ZeroPoint[0])
	}

	inScale := float64(0)
	if len(inT.Quant.Scale) > 0 {
		inScale = float64(inT.Quant.Scale[0])
	}
	outScale := float64(0)
	if len(outT.Quant.Scale) > 0 {
		outScale = float64(outT.Quant.Scale[0])
	}
	// FC may have per-channel or single scale
	qmuls := make([]QuantMultiplier, outputSize)
	for c := 0; c < outputSize; c++ {
		wScale := float64(0)
		if c < len(weightT.Quant.Scale) {
			wScale = float64(weightT.Quant.Scale[c])
		} else if len(weightT.Quant.Scale) > 0 {
			wScale = float64(weightT.Quant.Scale[0])
		}
		if outScale == 0 {
			outScale = 1.0
		}
		qmuls[c] = QuantizeMultiplier(inScale * wScale / outScale)
	}

	actMin, actMax := activationBounds(activation, outputZP)

	for b := 0; b < batch; b++ {
		for oc := 0; oc < outputSize; oc++ {
			acc := int32(getBias32(biasData, oc))
			for ic := 0; ic < inputSize; ic++ {
				inVal := int32(int8(inData[b*inputSize+ic])) - inputZP
				wVal := int32(int8(weightData[oc*inputSize+ic]))
				acc += inVal * wVal
			}
			acc = MultiplyByQuantizedMultiplier(acc, qmuls[oc])
			acc += outputZP
			if acc < actMin {
				acc = actMin
			}
			if acc > actMax {
				acc = actMax
			}
			outData[b*outputSize+oc] = byte(int8(ClampInt8(acc)))
		}
	}
	return nil
}
