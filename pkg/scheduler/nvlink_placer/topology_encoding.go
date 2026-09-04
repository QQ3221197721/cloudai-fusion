package nvlink_placer

// encodeEdgeKey creates integer key from two GPU indices (eliminates string allocations)
func encodeEdgeKey(gpu1, gpu2 uint8) uint64 {
	if gpu1 > gpu2 {
		gpu1, gpu2 = gpu2, gpu1
	}
	return uint64(gpu1)<<32 | uint64(gpu2)
}

// decodeEdgeKey extracts original GPU indices from encoded key
func decodeEdgeKey(key uint64) (uint8, uint8) {
	return uint8(key>>32), uint8(key & 0xffffffff)
}
