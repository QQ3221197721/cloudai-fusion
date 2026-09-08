package eventbus

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

const (
	DefaultArenaSize uint64 = 64 << 20
	MinArenaSize     uint64 = 1 << 20
	MaxArenaSize     uint64 = 1 << 30
)

type Arena struct {
	buffer []byte
	cursor int64
}

func NewArena(size uint64) *Arena {
	if size < MinArenaSize || size > MaxArenaSize {
		panic("invalid arena size")
	}
	return &Arena{
		buffer: make([]byte, size),
		cursor: 0,
	}
}

func (a *Arena) Alloc(n int) unsafe.Pointer {
	if n <= 0 {
		panic("alloc size must be positive")
	}
	
	pos := atomic.AddInt64(&a.cursor, int64(n))
	
	if pos+int64(n) > int64(len(a.buffer)) {
		panic(fmt.Sprintf("arena exhausted: requested %d bytes, only %d remaining", n, len(a.buffer)-int(pos)))
	}
	
	return unsafe.Pointer(&a.buffer[pos])
}

func (a *Arena) Reset() {
	atomic.StoreInt64(&a.cursor, 0)
}

func (a *Arena) Size() int {
	return len(a.buffer)
}

func (a *Arena) Stats() ArenaStatistics {
	used := atomic.LoadInt64(&a.cursor)
	size := a.Size()
	utilization := float64(0)
	if size > 0 {
		utilization = float64(used) / float64(size) * 100.0
	}
	
	return ArenaStatistics{
		Size:        size,
		UsedBytes:   used,
		AvailableBytes: int64(size) - used,
		Utilization: utilization,
	}
}

type ArenaStatistics struct {
	Size           int
	UsedBytes      int64
	AvailableBytes int64
	Utilization    float64
}
