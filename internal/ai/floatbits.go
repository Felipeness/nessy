package ai

import "math"

func floatToBitsViaConv(f float32) uint32 { return math.Float32bits(f) }
func bitsToFloatViaConv(b uint32) float32 { return math.Float32frombits(b) }
