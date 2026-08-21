import ReactECharts from 'echarts-for-react'
import { Card, Alert, Row, Col, Statistic } from 'antd'
import type { SchedulerStats } from '../types'
import { DashboardPage } from '../components/DashboardPage'
import { getSchedulerStats } from '../lib/moduleData'
import './GpuTopology.css'

// 真实数据来源：docs/final-hardware-validation/results/M3_TOPOLOGY_VALIDATION_REPORT.md §5
const BENCHMARK_DATA = [
  // k | exact-bnb | greedy-2opt | binpack | first-fit | k8s-default | random
  { k: 2, exactBnb: 3053, greedy2opt: 1625, binpack: 267.5, firstFit: 116.9, k8sDefault: 270.5, random: 216.3 },
  { k: 3, exactBnb: 4941, greedy2opt: 2415, binpack: 271.8, firstFit: 122.2, k8sDefault: 276.8, random: 239.1 },
  { k: 4, exactBnb: 6198, greedy2opt: 3182, binpack: 278.6, firstFit: 127.8, k8sDefault: 284.5, random: 248.5 },
  { k: 5, exactBnb: 13920, greedy2opt: 3821, binpack: 289.6, firstFit: 139.5, k8sDefault: 297.6, random: 271.6 },
  { k: 6, exactBnb: 10408, greedy2opt: 4291, binpack: 295.5, firstFit: 145.7, k8sDefault: 307.9, random: 291.6 },
  { k: 7, exactBnb: 4684, greedy2opt: 4517, binpack: 307.5, firstFit: 156.6, k8sDefault: 322.2, random: 319.3 },
  { k: 8, exactBnb: 1426, greedy2opt: 4292, binpack: 316.1, firstFit: 166.0, k8sDefault: 0, random: 0 },
]

// NVLink 拓扑矩阵（8-GPU 节点模拟，模拟一个典型的 DGX/HGX 拓扑）
// 布局：GPU0-3 在一个 NVSwitch 岛内 (900 GB/s)，GPU4-7 在另一个岛内
// 同岛 GPU 间用 NVLink 连接 (600 GB/s)，跨岛用 PCIe Gen4 x16 (32 GB/s)
const TOPOLOGY_MATRIX: number[][] = [
  //   0    1    2    3    4    5    6    7
  [ 0,   600,  600,  600, 32,   32,   32,   32  ], // GPU0
  [600,  0,   600,  600, 32,   32,   32,   32  ], // GPU1
  [600, 600,  0,   600, 32,   32,   32,   32  ], // GPU2
  [600, 600, 600,  0,   32,   32,   32,   32  ], // GPU3
  [32,  32,   32,   32,  0,   600,  600,  600 ], // GPU4
  [32,  32,   32,   32,  600, 0,   600,  600 ], // GPU5
  [32,  32,   32,   32,  600, 600, 0,   600 ], // GPU6
  [32,  32,   32,   32,  600, 600, 600, 0   ], // GPU7
]

// Build NVLink topology heatmap matrix
function buildTopologyHeatmap(): any {
  const labels = Array.from({ length: 8 }, (_, i) => `GPU${i}`)
  const seriesData = TOPOLOGY_MATRIX.map((row, i) => ({
    name: `GPU${i}`,
    type: 'heatmap',
    data: row.map((val, j) => ({ 0: i, 1: j, value: val })),
    itemStyle: {
      borderColor: '#fff',
      borderWidth: 2,
    },
  }))

  return {
    tooltip: {
      position: 'top',
      formatter: (params: any) => {
        const bw = params.value[2]
        let tier = '无连接'
        if (bw >= 900) tier = 'NVSwitch (900 GB/s)'
        else if (bw >= 600) tier = 'NVLink (600 GB/s)'
        else if (bw >= 32) tier = 'PCIe Gen4 x16 (32 GB/s)'
        return `<strong>${params.value[0]}</strong> → <strong>${params.value[1]}</strong><br/>带宽：${bw} GB/s<br/>层级：${tier}`
      },
    },
    grid: { height: '70%', top: '10%' },
    xAxis: { type: 'category', name: 'From', nameLocation: 'middle', nameGap: 25, data: labels },
    yAxis: { type: 'category', name: 'To', nameLocation: 'middle', nameGap: 25, inverse: true, data: labels },
    visualMap: {
      min: 0,
      max: 900,
      calculable: true,
      orient: 'horizontal',
      left: 'center',
      bottom: '0%',
      inRange: {
        color: ['#d9d9d9', '#69c0ff', '#fadb14', '#ff4d4f'],
      },
      textStyle: { color: '#fff' },
    },
    series: seriesData as any[],
  }
}

// Build solver latency comparison bar chart
function buildLatencyChart(): any {
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { data: ['exact-B&B', 'greedy-2opt'] },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'value', name: '耗时 (ns)', nameLocation: 'middle', nameGap: 25, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'category', data: Array.from({ length: 7 }, (_, i) => `k=${i + 2}`), axisLine: { lineStyle: { color: '#aab' } } },
    series: [
      {
        name: 'exact-B&B',
        type: 'bar',
        data: BENCHMARK_DATA.map((d) => d.exactBnb),
        itemStyle: { color: '#ff4d4f' },
        label: { show: true, position: 'right', formatter: (v: any) => `${v} ns` },
      },
      {
        name: 'greedy-2opt',
        type: 'bar',
        data: BENCHMARK_DATA.map((d) => d.greedy2opt),
        itemStyle: { color: '#16c784' },
        label: { show: true, position: 'right', formatter: (v: any) => `${v} ns` },
      },
    ],
  }
}

// Build placement quality comparison
function buildQualityComparison(): any {
  return {
    title: { text: 'TopologyAware vs K8s Default — NVLink Affinity (%)', left: 'center' },
    tooltip: { trigger: 'item' },
    legend: { orient: 'horizontal', bottom: '5%', data: ['TopologyAware', 'K8s Default'] },
    series: [
      {
        type: 'pie',
        radius: '65%',
        data: [
          { value: 100.0, name: 'TopologyAware', itemStyle: { color: '#16c784' } },
          { value: 66.8, name: 'K8s Default (BinPack)', itemStyle: { color: '#f5a623' } },
          { value: 64.2, name: 'K8s Default (Spread)', itemStyle: { color: '#faad14' } },
        ],
        label: { formatter: '{b}: {c}%' },
      },
    ],
  }
}

// Build bandwidth tier legend
function buildLegendInfo(): JSX.Element {
  return (
    <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
      <Col span={8}>
        <Card bodyStyle={{ padding: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div style={{ width: 24, height: 24, background: '#ff4d4f', borderRadius: 4 }} />
            <div>
              <div style={{ fontWeight: 'bold' }}>NVSwitch</div>
              <div style={{ fontSize: 12, color: '#9aa5b1' }}>900 GB/s (内部互联)</div>
            </div>
          </div>
        </Card>
      </Col>
      <Col span={8}>
        <Card bodyStyle={{ padding: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div style={{ width: 24, height: 24, background: '#fadb14', borderRadius: 4 }} />
            <div>
              <div style={{ fontWeight: 'bold' }}>NVLink 3.0</div>
              <div style={{ fontSize: 12, color: '#9aa5b1' }}>600 GB/s (GPU-GPU)</div>
            </div>
          </div>
        </Card>
      </Col>
      <Col span={8}>
        <Card bodyStyle={{ padding: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div style={{ width: 24, height: 24, background: '#69c0ff', borderRadius: 4 }} />
            <div>
              <div style={{ fontWeight: 'bold' }}>PCIe Gen4 x16</div>
              <div style={{ fontSize: 12, color: '#9aa5b1' }}>32 GB/s (跨岛/跨节点)</div>
            </div>
          </div>
        </Card>
      </Col>
    </Row>
  )
}

// Build benchmark stats
function buildBenchmarkStats(): JSX.Element {
  const k5Data = BENCHMARK_DATA.find((d) => d.k === 5)!
  const speedupRatio = (k5Data.exactBnb / k5Data.greedy2opt).toFixed(1)
  
  return (
    <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
      <Col xs={24} sm={12} md={6}>
        <Card>
          <Statistic 
            title="Exact-B&B (k=5)" 
            value={k5Data.exactBnb.toLocaleString()} 
            suffix=" ns"
            valueStyle={{ color: '#ff4d4f', fontWeight: 'bold' }}
          />
        </Card>
      </Col>
      <Col xs={24} sm={12} md={6}>
        <Card>
          <Statistic 
            title="Greedy-2opt (k=5)" 
            value={k5Data.greedy2opt.toLocaleString()} 
            suffix=" ns"
            valueStyle={{ color: '#16c784', fontWeight: 'bold' }}
          />
        </Card>
      </Col>
      <Col xs={24} sm={12} md={6}>
        <Card>
          <Statistic 
            title="Speedup Ratio" 
            value={speedupRatio} 
            suffix="×"
            valueStyle={{ color: '#69c0ff', fontWeight: 'bold' }}
          />
        </Card>
      </Col>
      <Col xs={24} sm={12} md={6}>
        <Card>
          <Statistic 
            title="最优解保证" 
            value="100%"
            valueStyle={{ color: '#722ed1', fontWeight: 'bold' }}
          />
        </Card>
      </Col>
    </Row>
  )
}

export function TopologyScheduler(): JSX.Element {
  const placeholder = () => (
    <>
      <Alert 
        type="success" 
        message={<><strong>Finding:</strong> greedy-2opt achieves near-optimal solutions on real GPU topologies</>}
      />
      <Card className="ratio-cards">
        <div className="ratio-card">
          <span className="card-label">Mean Approximation Ratio</span>
          <span className="card-value">99.9954%</span>
        </div>
        <div className="ratio-card">
          <span className="card-label">Worst Case Ratio</span>
          <span className="card-value">97.2027%</span>
        </div>
        <div className="ratio-card">
          <span className="card-label">95% CI</span>
          <span className="card-value">[99.9895%, 100.0013%]</span>
        </div>
      </Card>
      
      <div style={{ marginBottom: 16 }}><h3>NVLink 拓扑矩阵热力图 (8×8 模拟 DGX/HGX 拓扑)</h3></div>
      <ReactECharts option={buildTopologyHeatmap()} style={{ height: 400 }} />
      
      <div style={{ marginBottom: 16 }}><h3>算法性能对比</h3></div>
      <ReactECharts option={buildLatencyChart()} style={{ height: 400 }} />
      
      <div style={{ marginBottom: 16 }}><h3>放置质量对比：NVLink 亲和性 (TopologyAware 100% vs K8s 66.8%)</h3></div>
      <ReactECharts option={buildQualityComparison()} style={{ height: 320 }} />
      
      {buildLegendInfo()}
      {buildBenchmarkStats()}
    </>
  )

  return (
    <DashboardPage
      title="M3 — GPU Topology-Aware Scheduling"
      subtitle="dense-k-subgraph 放置可视化 · NVLink 亲和性分析 · Exact-B&B vs Greedy-2opt 性能对比"
      backendModule="pkg/scheduler/dense_k_subgraph.go"
      loader={getSchedulerStats}
      isEmpty={(data: SchedulerStats) => data.results.length === 0}
      dataSourceNote="REAL measured numbers from docs/final-hardware-validation/results/M3_TOPOLOGY_VALIDATION_REPORT.md §5 (single A100 VM, synthetic 16-GPU/4-island topology model). NVLink/NVSwitch tiers spec-correct but needs-8xGPU to measure directly."
      children={placeholder}
    />
  )
}
