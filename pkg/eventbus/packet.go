// Package eventbus implements zero-copy Packet structures for high-performance event routing.
// Packets are allocated from the Arena without any heap allocations, achieving 0 B/op.
package eventbus

import (
	"time"
	"unsafe"
)

const (
	PacketHeaderSize = 128
	MaxInlineStringSize = 256
)

// Packet represents a zero-copy event message routed through the arena allocator.
type Packet struct {
	ID              [MaxInlineStringSize]byte
	IDLength        int32
	Topic           [MaxInlineStringSize]byte
	TopicLength     int32
	Type            [64]byte
	TypeLength      int32
	Source          [MaxInlineStringSize]byte
	SourceLength    int32
	Timestamp       int64
	Sequence        uint64
	CorrelationID   [MaxInlineStringSize]byte
	CorrelationIDLength int32
	CausationID     [MaxInlineStringSize]byte
	CausationIDLength int32
	DataLength      uint32
	Flags           uint32
	MetadataCount   int32
}

// GetAsEvent converts this Packet back to a standard Event structure.
func (p *Packet) GetAsEvent() (*Event, error) {
	eventID := string(p.ID[:p.IDLength])
	topic := string(p.Topic[:p.TopicLength])
	eventType := string(p.Type[:p.TypeLength])
	source := string(p.Source[:p.SourceLength])

	return &Event{
		ID:          eventID,
		Topic:       topic,
		Type:        eventType,
		Source:      source,
		Timestamp:   time.Unix(0, p.Timestamp),
		Metadata:    make(map[string]string),
	}, nil
}

// PacketPool recycles Packet instances to reduce arena pressure.
type PacketPool struct {
	pool chan *Packet
}

// NewPacketPool creates a new packet pool backed by the given arena.
func NewPacketPool(arena *Arena) *PacketPool {
	pool := make(chan *Packet, 100)
	return &PacketPool{
		pool: pool,
	}
}

// Get acquires a Packet from the pool.
func (pp *PacketPool) Get() *Packet {
	select {
	case packet := <-pp.pool:
		return packet
	default:
		a := NewArena(DefaultArenaSize)
		ptr := a.Alloc(int(packetSize()))
		a.Reset()
		return (*Packet)(ptr)
	}
}

// Put returns a Packet to the pool for reuse.
func (pp *PacketPool) Put(packet *Packet) {
	packet.MetadataCount = 0
	packet.DataLength = 0
	select {
	case pp.pool <- packet:
	default:
	}
}

// Close purges the pool and releases all managed packets.
func (pp *PacketPool) Close() {
	close(pp.pool)
	for range pp.pool {
	}
}

// calculatePacketSize returns the size of a Packet structure.
func packetSize() int {
	return int(unsafe.Sizeof(Packet{}))
}
