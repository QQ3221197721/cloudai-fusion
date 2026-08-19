// Package plugin — Task 104: in-process vs gRPC loopback comparison.
//
// This file benchmarks two isolation models with an identical, trivial "score"
// operation so the measured delta is pure transport overhead:
//
//  1. In-process: a direct ScorePlugin.Score method call through an interface —
//     the path this runtime actually uses.
//  2. Out-of-process: a REAL gRPC unary round trip over loopback TCP — the same
//     transport HashiCorp go-plugin uses (go-plugin adds process spawn + its own
//     framing on top, so this is a conservative lower bound on its cost).
//
// The gRPC service is built with a manual grpc.ServiceDesc + a raw-bytes codec,
// so no protoc / generated stubs are needed and no new dependency is added
// (google.golang.org/grpc is already required by go.mod).
//
// Honesty note: client and server share one process here, so this captures the
// serialization + loopback-syscall cost but NOT the extra process-scheduling
// cost a real go-plugin subprocess would add. Real go-plugin is therefore
// slower than these numbers, never faster.
//
// Run (PowerShell):
//   go test ./pkg/plugin/ "-bench=BenchmarkScore_" -benchmem -count=3 -benchtime=5x "-run=^$"
package plugin

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

// ============================================================================
// Shared "score" operation (identical work on both paths for fairness)
// ============================================================================

type scoreRequest struct {
	GPUFree uint64
	NodeID  string
	WalkID  []byte
}

type scoreResponse struct{ Score int64 }

// computeScore is the deterministic scoring function both paths execute.
func computeScore(req scoreRequest) scoreResponse {
	score := int64(100) - int64(req.GPUFree%100)
	for i := 0; i < len(req.WalkID); i++ {
		score += int64(req.WalkID[i])
	}
	return scoreResponse{Score: score % 101}
}

// Encode serializes a request into a compact wire format:
// [8B gpuFree][2B len(nodeID)][nodeID][4B len(walkID)][walkID].
func (s scoreRequest) Encode() []byte {
	buf := make([]byte, 0, 8+2+len(s.NodeID)+4+len(s.WalkID))
	buf = binary.BigEndian.AppendUint64(buf, s.GPUFree)
	buf = append(buf, byte(len(s.NodeID)>>8), byte(len(s.NodeID)))
	buf = append(buf, s.NodeID...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(s.WalkID)))
	buf = append(buf, s.WalkID...)
	return buf
}

func decodeRequest(data []byte) (scoreRequest, error) {
	if len(data) < 14 {
		return scoreRequest{}, fmt.Errorf("score: short request (%d bytes)", len(data))
	}
	gpuFree := binary.BigEndian.Uint64(data)
	off := 8
	nodeLen := int(data[off])<<8 | int(data[off+1])
	off += 2
	if off+nodeLen+4 > len(data) {
		return scoreRequest{}, fmt.Errorf("score: malformed node segment")
	}
	nodeID := string(data[off : off+nodeLen])
	off += nodeLen
	walkLen := int(binary.BigEndian.Uint32(data[off:]))
	off += 4
	if off+walkLen > len(data) {
		return scoreRequest{}, fmt.Errorf("score: malformed walk segment")
	}
	return scoreRequest{GPUFree: gpuFree, NodeID: nodeID, WalkID: data[off : off+walkLen]}, nil
}

func encodeResponse(resp scoreResponse) []byte {
	return binary.BigEndian.AppendUint64(make([]byte, 0, 8), uint64(resp.Score))
}

// ============================================================================
// In-process path
// ============================================================================

// benchScorePlugin implements just enough of ScorePlugin to time a direct call.
type benchScorePlugin struct{ BasePlugin }

func newBenchScorePlugin() *benchScorePlugin {
	return &benchScorePlugin{BasePlugin: NewBasePlugin(Metadata{
		Name:            "bench-score",
		ExtensionPoints: []ExtensionPoint{ExtSchedulerScore},
	})}
}

func (p *benchScorePlugin) Score(_ context.Context, _ *CycleState, workload *WorkloadInfo, node *NodeInfo) (int64, *Result) {
	req := scoreRequest{GPUFree: uint64(node.GPUFree), NodeID: node.ClusterID, WalkID: []byte(workload.ID)}
	return computeScore(req).Score, SuccessResult(p.meta.Name)
}

func (p *benchScorePlugin) ScoreWeight() int64 { return 1 }

var _ ScorePlugin = (*benchScorePlugin)(nil)

// BenchmarkScore_InProcess measures the steady-state in-process call path: look
// the plugin up as a ScorePlugin and invoke Score directly. No serialization,
// no syscalls, no process boundary.
func BenchmarkScore_InProcess(b *testing.B) {
	ctx := context.Background()
	p := newBenchScorePlugin()
	state := NewCycleState()
	workload := &WorkloadInfo{ID: "wl1", Priority: 10}
	node := &NodeInfo{ClusterID: "n1", GPUFree: 3, GPUTotal: 8}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, r := p.Score(ctx, state, workload, node); !r.IsSuccess() {
			b.Fatalf("score failed: %v", r)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/s")
}

// BenchmarkScore_InProcessViaRegistry adds the realistic lookup: fetch the
// plugin from the Registry by extension point, then call Score. This is the
// production dispatch path (map lookup + interface assertion + call).
func BenchmarkScore_InProcessViaRegistry(b *testing.B) {
	ctx := context.Background()
	r := NewRegistry()
	if err := r.Register("bench-score", func() (Plugin, error) { return newBenchScorePlugin(), nil }); err != nil {
		b.Fatalf("register: %v", err)
	}
	if _, err := r.Build(); err != nil {
		b.Fatalf("build: %v", err)
	}
	state := NewCycleState()
	workload := &WorkloadInfo{ID: "wl1", Priority: 10}
	node := &NodeInfo{ClusterID: "n1", GPUFree: 3, GPUTotal: 8}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, pl := range r.GetByExtension(ExtSchedulerScore) {
			sp, ok := pl.(ScorePlugin)
			if !ok {
				b.Fatal("plugin does not implement ScorePlugin")
			}
			if _, res := sp.Score(ctx, state, workload, node); !res.IsSuccess() {
				b.Fatalf("score failed: %v", res)
			}
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/s")
}

// ============================================================================
// gRPC loopback path (real TCP + serialization, no protoc)
// ============================================================================

const rawBytesCodecName = "cloudai-rawbytes"

// rawBytesCodec is a gRPC codec that transports []byte payloads verbatim, so a
// unary method can be exercised without protobuf-generated types.
type rawBytesCodec struct{}

func (rawBytesCodec) Marshal(v any) ([]byte, error) {
	switch t := v.(type) {
	case []byte:
		return t, nil
	case *[]byte:
		return *t, nil
	default:
		return nil, fmt.Errorf("rawBytesCodec: cannot marshal %T", v)
	}
}

func (rawBytesCodec) Unmarshal(data []byte, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawBytesCodec: cannot unmarshal into %T", v)
	}
	*p = append((*p)[:0], data...)
	return nil
}

func (rawBytesCodec) Name() string { return rawBytesCodecName }

var registerCodecOnce sync.Once

// scoreMethodHandler decodes the request bytes, runs the shared scoring
// function, and returns the response bytes for the codec to marshal.
func scoreMethodHandler(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	var in []byte
	if err := dec(&in); err != nil {
		return nil, err
	}
	req, err := decodeRequest(in)
	if err != nil {
		return nil, err
	}
	return encodeResponse(computeScore(req)), nil
}

// benchScoreServiceDesc registers a single unary method /bench.Score/Score.
var benchScoreServiceDesc = grpc.ServiceDesc{
	ServiceName: "bench.Score",
	HandlerType: (*any)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "Score", Handler: scoreMethodHandler}},
	Streams:     []grpc.StreamDesc{},
	Metadata:    "task104",
}

// BenchmarkScore_GRPCLoopback measures a real gRPC unary round trip over
// loopback TCP: marshal → TCP send → server dispatch → handler → marshal reply
// → TCP recv → unmarshal. This is the transport cost every out-of-process
// plugin call pays.
func BenchmarkScore_GRPCLoopback(b *testing.B) {
	registerCodecOnce.Do(func() { encoding.RegisterCodec(rawBytesCodec{}) })

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Skipf("Skipping (listen failed): %v", err)
	}
	srv := grpc.NewServer()
	srv.RegisterService(&benchScoreServiceDesc, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Skipf("Skipping (dial failed): %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	req := scoreRequest{GPUFree: 3, NodeID: "n1", WalkID: []byte("wl1")}.Encode()

	// Warmup: first Invoke triggers the TCP connect + HTTP/2 handshake, which we
	// exclude from the timed region.
	var warm []byte
	if err := conn.Invoke(ctx, "/bench.Score/Score", req, &warm, grpc.CallContentSubtype(rawBytesCodecName)); err != nil {
		b.Skipf("Skipping (warmup invoke failed): %v", err)
	}
	if len(warm) != 8 {
		b.Fatalf("unexpected reply length %d", len(warm))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var reply []byte
		if err := conn.Invoke(ctx, "/bench.Score/Score", req, &reply, grpc.CallContentSubtype(rawBytesCodecName)); err != nil {
			b.Fatalf("invoke: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/s")
}
