import { Link } from 'react-router-dom'
import { Card, Row, Col } from 'antd'
import { ThunderboltOutlined, SafetyCertificateOutlined, DashboardOutlined, ClusterOutlined } from '@ant-design/icons'
import './Overview.css'

const CARDS = [
  {
    to: '/capabilities',
    icon: <ClusterOutlined />,
    title: 'Capabilities',
    desc: 'Per-subsystem real vs simulated status from GET /api/v1/capabilities. See exactly which backends are live.',
    accent: '#16c784',
  },
  {
    to: '/gpu',
    icon: <DashboardOutlined />,
    title: 'GPU Utilization',
    desc: 'Node × GPU heatmap of utilization percentages. (Mock data — no GPU endpoint yet.)',
    accent: '#2fd4d4',
  },
  {
    to: '/evidence',
    icon: <SafetyCertificateOutlined />,
    title: 'Evidence Validation',
    desc: 'Load a local evidence chain export and validate hash-chain linkage, signatures, and Merkle root.',
    accent: '#f5a623',
  },
]

export function Overview(): JSX.Element {
  return (
    <div className="overview-container">
      <div className="hero">
        <span className="hero-eyebrow">OPERATIONS CONSOLE · MODULES 39–44</span>
        <h1 className="hero-title">Observe. Verify. Trust.</h1>
        <p className="hero-sub">
          An honesty-first control surface for the CloudAI Fusion platform. Every panel discloses whether it is
          showing real backend data or a labeled mock — no silent faking.
        </p>
      </div>

      <Row gutter={[20, 20]} className="cards-grid">
        {CARDS.map((c) => (
          <Col xs={24} md={8} key={c.to}>
            <Link to={c.to} className="feature-link">
              <Card className="feature-card" bodyStyle={{ padding: 24 }} hoverable>
                <div className="feature-icon" style={{ color: c.accent }}>
                  {c.icon}
                </div>
                <h3 className="feature-title">{c.title}</h3>
                <p className="feature-desc">{c.desc}</p>
                <div className="feature-accent" style={{ background: c.accent }} />
              </Card>
            </Link>
          </Col>
        ))}
      </Row>

      <div className="tips-panel">
        <ThunderboltOutlined style={{ color: '#f5a623' }} />
        <span>First visit? A 4-step interactive tutorial will guide you through the console. You can skip it anytime.</span>
      </div>
    </div>
  )
}
