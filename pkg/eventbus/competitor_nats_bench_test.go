package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// ============================================================================
// Module 6 T2 定位性胜出 benchmark: 内嵌 NATS broker vs 我方零分配 WellRouter
// ============================================================================
//
// 目的: 把 pkg/eventbus 的零分配 WellRouter/FastRouter 与一个真实的 in-process
// NATS broker (nats-server/v2 v2.10.22 + nats.go v1.37.0) 做头对头对比，验证
// Module 6 的 T2 定位性主张：
//
//   - 我方主打「进程内(in-process)超低延迟、零堆分配、可离线验签」的事件路由；
//   - NATS 是完整的分布式 broker（跨进程/跨节点、持久化、消费组、DLQ）。
//     两者解决的是不同层次的问题，本基准只在「同机进程内 publish→subscribe
//     单跳路由延迟 + allocs/op」这一可比维度上做诚实对比。
//
// 为了公平，NATS 侧使用 core NATS（nc.Publish/nc.Subscribe，fire-and-forget，
// 不含 JetStream 持久化开销）——这是 NATS 能给出的最低延迟路径，对我方最不利、
// 因此最诚实。JetStream(持久化)路径见既有 BenchmarkNATSBus_* 基准。
//
// 运行命令（PowerShell，分隔用分号）:
//   go test ./pkg/eventbus/... -run=^$ -bench='BenchmarkCompetitorNATS_InProcess_SingleHop|BenchmarkChannel_PubSub' -benchmem -benchtime=10x -json
// ============================================================================

// startEmbeddedNATS 在随机端口启动一个进程内 NATS server（core NATS，无需
// JetStream/持久化），返回 server 与其客户端 URL。调用方必须 Shutdown。
func startEmbeddedNATS(tb testing.TB) (*server.Server, string) {
	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // 随机可用端口
		NoLog:     true,
		NoSigs:    true,
		JetStream: false, // core NATS：最低延迟路径
	}
	srv, err := server.NewServer(opts)
	if err != nil {
		tb.Fatalf("create embedded NATS server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		tb.Fatal("embedded NATS did not start within 10s")
	}
	return srv, srv.ClientURL()
}

// BenchmarkCompetitorNATS_InProcess_SingleHop 测量一次 core NATS publish 到本地
// 单个订阅者的端到端往返延迟。NATS 投递是异步的，为避免 busy-wait 轮询粒度
// 污染测量，订阅回调通过一个带缓冲的 signal channel 通知发布方，发布方阻塞
// 等待 <-sig，因此测到的是真实的 publish→deliver 延迟而非 sleep 粒度。
func BenchmarkCompetitorNATS_InProcess_SingleHop(b *testing.B) {
	srv, url := startEmbeddedNATS(b)
	defer srv.Shutdown()

	nc, err := nats.Connect(url)
	if err != nil {
		b.Fatalf("connect embedded NATS: %v", err)
	}
	defer nc.Close()

	const topic = "well.t2.singlehop"
	sig := make(chan struct{}, 1)
	var received atomic.Int64
	sub, err := nc.Subscribe(topic, func(msg *nats.Msg) {
		received.Add(1)
		sig <- struct{}{}
	})
	if err != nil {
		b.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	// 确保订阅已在 server 注册，避免早发消息丢失。
	if err := nc.Flush(); err != nil {
		b.Fatalf("flush: %v", err)
	}

	payload := []byte("cve_ingested")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := nc.Publish(topic, payload); err != nil {
			b.Fatalf("publish: %v", err)
		}
		<-sig // 阻塞直到订阅者消费这一条，测真实端到端延迟
	}
	b.StopTimer()

	if received.Load() < int64(b.N) {
		b.Fatalf("delivered %d/%d", received.Load(), b.N)
	}
	sec := b.Elapsed().Seconds()
	if sec > 0 {
		b.ReportMetric(float64(b.N)/sec, "events/sec")
	}
}

// BenchmarkChannel_PubSub 是标准库 channel 的进程内 pub/sub 基线：一个无锁的
// 有缓冲 channel + 单消费者 goroutine。它代表「不用任何 broker，纯 Go 原语」
// 的下限，用来给 NATS 与我方 WellRouter 之间的数字提供参照系。
func BenchmarkChannel_PubSub(b *testing.B) {
	ch := make(chan []byte, 1024)
	var received atomic.Int64
	done := make(chan struct{})
	go func() {
		for range ch {
			received.Add(1)
		}
		close(done)
	}()

	payload := []byte("cve_ingested")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- payload
	}
	b.StopTimer()
	close(ch)
	<-done

	if received.Load() != int64(b.N) {
		b.Fatalf("delivered %d/%d", received.Load(), b.N)
	}
	sec := b.Elapsed().Seconds()
	if sec > 0 {
		b.ReportMetric(float64(b.N)/sec, "events/sec")
	}
}
