import { Card, Row, Col, Tag, Statistic, Table, Alert, Typography, Badge } from 'antd'
import ReactECharts from 'echarts-for-react'
import type { ColumnsType } from 'antd/es/table'

const { Title, Text, Paragraph } = Typography

// Scaler policies (pkg/scaler/scaler.go)
interface ScalePolicy {
  id: string
  name: string
  metric: 'latency_p95' | 'accuracy' | 'throughput' | 'error_rate'
  threshold: number
  direction: 'regression_triggers_up'
  minNodes: number
  maxNodes: number
  cooldownMinutes: number
  enabled: boolean
  createdAt: string
}

// ScaleDecision (pkg/scaler/scaler.go)
interface ScaleDecision {
  id: string
  action: 'scale_up' | 'scale_down' | 'no_change'
  reason: string
  triggerSource: 'monitor_alert' | 'experiment_comparison' | 'manual' | 'budget_enforcement'
  currentNodes: number
  targetNodes: number
  costImpactPerHour: number
  budgetOK: boolean
  applied: boolean
  createdAt: string
  appliedAt?: string
}

interface AutoscaleMetrics {
  activePolicies: number
  decisionsToday: number
  avgCostSavings: number
  scaleUpCount: number
  scaleDownCount: number
  noChangeCount: number
}

// Mock policy data (A100 GPU cluster scaling policies)
const mockPolicies: ScalePolicy[] = [
  {
    id: 'pol-a1b2c3d4',
    name: 'Latency Protection',
    metric: 'latency_p95',
    threshold: 85,
    direction: 'regression_triggers_up',
    minNodes: 2,
    maxNodes: 16,
    cooldownMinutes: 10,
    enabled: true,
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 24 * 7).toISOString(),
  },
  {
    id: 'pol-e5f6g7h8',
    name: 'Accuracy Threshold',
    metric: 'accuracy',
    threshold: 92,
    direction: 'regression_triggers_up',
    minNodes: 1,
    maxNodes: 8,
    cooldownMinutes: 15,
    enabled: true,
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 24 * 5).toISOString(),
  },
  {
    id: 'pol-i9j0k1l2',
    name: 'Budget Enforcement',
    metric: 'error_rate',
    threshold: 5,
    direction: 'regression_triggers_up',
    minNodes: 1,
    maxNodes: 4,
    cooldownMinutes: 5,
    enabled: false,
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 24 * 2).toISOString(),
  },
]

// Mock decision history
const mockDecisions: ScaleDecision[] = [
  {
    id: 'sd-m1n2o3p4',
    action: 'scale_up',
    reason: 'P95 latency exceeded 88% threshold (current: 92ms)',
    triggerSource: 'monitor_alert',
    currentNodes: 4,
    targetNodes: 6,
    costImpactPerHour: 3.60,
    budgetOK: true,
    applied: true,
    createdAt: new Date(Date.now() - 1000 * 60 * 5).toISOString(),
    appliedAt: new Date(Date.now() - 1000 * 60 * 3).toISOString(),
  },
  {
    id: 'sd-q5r6s7t8',
    action: 'no_change',
    reason: 'Accuracy gain 1.5% < 2pp upgrade threshold',
    triggerSource: 'experiment_comparison',
    currentNodes: 8,
    targetNodes: 8,
    costImpactPerHour: 0,
    budgetOK: true,
    applied: true,
    createdAt: new Date(Date.now() - 1000 * 60 * 30).toISOString(),
    appliedAt: new Date(Date.now() - 1000 * 60 * 28).toISOString(),
  },
  {
    id: 'sd-u9v0w1x2',
    action: 'scale_down',
    reason: 'Throughput utilization below 20% for 30 minutes',
    triggerSource: 'monitor_alert',
    currentNodes: 8,
    targetNodes: 4,
    costImpactPerHour: -3.60,
    budgetOK: true,
    applied: true,
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
    appliedAt: new Date(Date.now() - 1000 * 60 * 60 * 1.8).toISOString(),
  },
]

// Metrics summary
const mockMetrics: AutoscaleMetrics = {
  activePolicies: 2,
  decisionsToday: 3,
  avgCostSavings: 12.50,
  scaleUpCount: 8,
  scaleDownCount: 5,
  noChangeCount: 12,
}

// Build policy chart
function buildPolicyDistributionChart(): Record<string, unknown> {
  return {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, textStyle: { color: '#aab' } },
    series: [
      {
        name: 'Policy Status',
        type: 'pie',
        radius: ['45%', '70%'],
        label: { color: '#ccd' },
        data: [
          { value: mockMetrics.activePolicies, name: 'Active', itemStyle: { color: '#52c41a' } },
          { value: mockPolicies.length - mockMetrics.activePolicies, name: 'Disabled', itemStyle: { color: '#ff4d4f' } },
        ],
      },
    ],
  }
}

// Build timeline chart
function buildTimelineChart(decisions: ScaleDecision[]): Record<string, unknown> {
  const categories = decisions.map(d => d.createdAt)
  const actions = decisions.map(d => d.action === 'scale_up' ? 1 : d.action === 'scale_down' ? -1 : 0)

  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    xAxis: {
      type: 'category',
      data: categories,
      axisLabel: { color: '#aab', rotate: 45, fontSize: 10 },
    },
    yAxis: {
      type: 'value',
      axisLabel: { color: '#aab' },
      name: 'Node Change',
    },
    series: [
      {
        name: 'Scaling Events',
        type: 'bar',
        data: actions,
        itemStyle: {
          color: (params: any) => {
            if (params.value > 0) return '#52c41a'
            if (params.value < 0) return '#faad14'
            return '#8c8c8c'
          },
        },
      },
    ],
  }
}

export function AutoscaleEngine(): JSX.Element {
  const data = {
    data: { metrics: mockMetrics, policies: mockPolicies, decisions: mockDecisions },
    source: 'mock' as const,
    reason: 'Backend API endpoint /api/v1/scaler/* not implemented yet — showing sample scaler policies/decisions shaped to pkg/scaler/scaler.go types.',
    fetchedAt: new Date().toISOString(),
  }

  const policyColumns: ColumnsType<ScalePolicy> = [
    {
      title: 'Policy Name',
      dataIndex: 'name',
      key: 'name',
      width: 180,
    },
    {
      title: 'Metric',
      dataIndex: 'metric',
      key: 'metric',
      width: 120,
      render: (metric: string) => <Tag color="blue">{metric}</Tag>,
    },
    {
      title: 'Threshold (%)',
      dataIndex: 'threshold',
      key: 'threshold',
      width: 100,
      align: 'center',
    },
    {
      title: 'Min→Max',
      key: 'minmax',
      width: 100,
      render: (_, record) => `${record.minNodes} → ${record.maxNodes}`,
      align: 'center',
    },
    {
      title: 'Cooldown',
      dataIndex: 'cooldownMinutes',
      key: 'cooldownMinutes',
      width: 100,
      align: 'center',
      render: (mins: number) => `${mins}m`,
    },
    {
      title: 'Status',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (enabled: boolean) => (
        <Badge status={enabled ? 'success' : 'default'} text={enabled ? 'Enabled' : 'Disabled'} />
      ),
    },
  ]

  const decisionColumns: ColumnsType<ScaleDecision> = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 140,
      render: (id: string) => <Text code>{id}</Text>,
    },
    {
      title: 'Action',
      dataIndex: 'action',
      key: 'action',
      width: 120,
      render: (action: string) => {
        const colors: Record<string, string> = {
          scale_up: 'green',
          scale_down: 'orange',
          no_change: 'default',
        }
        return <Tag color={colors[action] || 'default'}>{action}</Tag>
      },
    },
    {
      title: 'Reason',
      dataIndex: 'reason',
      key: 'reason',
      ellipsis: true,
    },
    {
      title: 'Nodes',
      key: 'nodes',
      width: 100,
      render: (_, record) => `${record.currentNodes} → ${record.targetNodes}`,
      align: 'center',
    },
    {
      title: 'Cost/Hour',
      dataIndex: 'costImpactPerHour',
      key: 'costImpactPerHour',
      width: 100,
      align: 'center',
      render: (cost: number) => (
        <Text type={cost > 0 ? 'danger' : cost < 0 ? 'success' : 'secondary'}>
          {cost > 0 ? '+' : ''}${cost.toFixed(2)}
        </Text>
      ),
    },
    {
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 180,
      render: (ts: string) => new Date(ts).toLocaleString('zh-CN'),
    },
  ]

  return (
    <div style={{ padding: '24px', background: '#f0f2f5', minHeight: '100%' }}>
      {/* Header Section */}
      <Card style={{ marginBottom: 24 }}>
        <Title level={3}>Auto-scaling Engine</Title>
        <Paragraph type="secondary">
          基于性能指标和预算约束的自动弹性扩缩决策引擎（M16）
        </Paragraph>
      </Card>

      {/* Warning Banner */}
      <Alert
        type="warning"
        showIcon
        message="数据源说明"
        description={data.reason}
        style={{ marginBottom: 24 }}
      />

      {/* Metrics Summary */}
      <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
        <Col span={6}>
          <Card>
            <Statistic
              title="活跃策略"
              value={data.data.metrics.activePolicies}
              suffix="/3"
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="今日决策"
              value={data.data.metrics.decisionsToday}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="平均节省"
              value={data.data.metrics.avgCostSavings}
              suffix="USD/hr"
              valueStyle={{ color: '#faad14' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="净伸缩"
              value={data.data.metrics.scaleUpCount - data.data.metrics.scaleDownCount}
              suffix={`up:${data.data.metrics.scaleUpCount}`}
              valueStyle={{ color: data.data.metrics.scaleUpCount >= data.data.metrics.scaleDownCount ? '#52c41a' : '#ff4d4f' }}
            />
          </Card>
        </Col>
      </Row>

      {/* Charts Section */}
      <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
        <Col span={12}>
          <Card title="策略分布" bodyStyle={{ textAlign: 'center' }}>
            <ReactECharts option={buildPolicyDistributionChart()} style={{ height: 280 }} />
          </Card>
        </Col>
        <Col span={12}>
          <Card title="伸缩时间线" bodyStyle={{ textAlign: 'center' }}>
            <ReactECharts option={buildTimelineChart(data.data.decisions)} style={{ height: 280 }} />
          </Card>
        </Col>
      </Row>

      {/* Tables Section */}
      <Row gutter={[16, 16]}>
        <Col span={12}>
          <Card title="扩展策略 (Scaling Policies)">
            <Table
              columns={policyColumns}
              dataSource={data.data.policies}
              rowKey="id"
              pagination={false}
              size="small"
            />
          </Card>
        </Col>
        <Col span={12}>
          <Card title="历史决策 (Decision History)">
            <Table
              columns={decisionColumns}
              dataSource={data.data.decisions}
              rowKey="id"
              pagination={false}
              size="small"
            />
          </Card>
        </Col>
      </Row>

      {/* Footer Info */}
      <Card style={{ marginTop: 24, background: '#f9f9f9' }}>
        <Title level={5}>后端实现路径</Title>
        <Paragraph style={{ fontSize: 13, marginBottom: 0 }}>
          真实后端端点：当前未暴露。计划实现 POST /api/v1/scaler/policies (创建策略), 
          GET /api/v1/scaler/policies (列表策略), 
          POST /api/v1/scaler/evaluate (触发评估), 
          GET /api/v1/scaler/history (历史决策)。
          数据模型参考：<code>pkg/scaler/scaler.go</code> 中的 <code>ScalePolicy</code> 和 <code>ScaleDecision</code> 类型。
        </Paragraph>
      </Card>
    </div>
  )
}
