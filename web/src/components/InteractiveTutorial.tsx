import { useState, useEffect } from 'react'
import { Modal, Button, Space, Steps, Typography } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'
import './Tutorial.css'

const { Text } = Typography

interface StepData {
  title: string
  description: React.ReactNode
  actionLabel?: string
}

const STEPS: StepData[] = [
  {
    title: 'Check Cluster Health',
    description: (
      <div>
        <p>Our platform provides a unified view of cluster health across all nodes and subsystems.</p>
        <ul style={{ marginLeft: 20 }}>
          <li>Monitor GPU topology and resource utilization</li>
          <li>Track evidence chain integrity and signing latency</li>
          <li>Observe real-time metrics for scheduler nodes and mesh services</li>
        </ul>
      </div>
    ),
    actionLabel: 'Next →',
  },
  {
    title: 'Verify Capability Status',
    description: (
      <div>
        <p>The capabilities panel surfaces whether each subsystem runs on a <strong>real backend</strong> or falls back to simulation.</p>
        <ul style={{ marginLeft: 20 }}>
          <li><strong>[REAL]</strong>: backed by Redis, Kubernetes, Rekor, etc.</li>
          <li><strong>[SIMULATED]</strong>: in-memory fallback for development/testing</li>
          <li>Always honest — no silent faking when backends are down</li>
        </ul>
      </div>
    ),
    actionLabel: 'Continue →',
  },
  {
    title: 'Deploy Test Workload',
    description: (
      <div>
        <p>You can deploy workloads while monitoring their impact on GPU resources and evidence chain generation.</p>
        <Text type="secondary" style={{ display: 'block', marginTop: 6 }}>Example actions:</Text>
        <ul style={{ marginLeft: 20 }}>
          <li>Trigger a new scheduling job via the API</li>
          <li>Inspect generated receipts in the evidence ledger</li>
          <li>Validate hashes against local bundle exports</li>
        </ul>
      </div>
    ),
    actionLabel: 'Review Evidence →',
  },
  {
    title: 'Examine Monitoring Metrics',
    description: (
      <div>
        <p>The final step is to review detailed metrics and dashboards.</p>
        <ul style={{ marginLeft: 20 }}>
          <li>Prometheus/Grafana panels for deeper inspection</li>
          <li>Custom alerts on latency spikes and error rates</li>
          <li>Evidence chain lag notifications</li>
        </ul>
      </div>
    ),
  },
]

export function InteractiveTutorial(): JSX.Element {
  const [open, setOpen] = useState(false)
  const [current, setCurrent] = useState(0)
  const hasSkipped: boolean = (() => {
    try {
      return localStorage.getItem('tutorial-skipped') === 'true'
    } catch {
      return false
    }
  })()

  useEffect(() => {
    // Show tutorial only once per session, unless skippable flag allows repeat
    try {
      const viewed = localStorage.getItem('tutorial-viewed')
      if (!viewed && !hasSkipped) {
        setOpen(true)
      }
    } catch {
      // localStorage unavailable → silently allow tutorial
    }
  }, [])

  const close = () => setOpen(false)

  const next = () => {
    if (current < STEPS.length - 1) {
      setCurrent(current + 1)
    } else {
      // Complete
      complete()
    }
  }

  const prev = () => {
    if (current > 0) setCurrent(current - 1)
    else close()
  }

  const complete = () => {
    try {
      localStorage.setItem('tutorial-viewed', 'true')
    } catch {}
    close()
    setCurrent(0)
  }

  const skip = () => {
    try {
      localStorage.setItem('tutorial-skipped', 'true')
    } catch {}
    close()
  }

  if (!open || hasSkipped) return <></>

  const step = STEPS[current]

  return (
    <Modal
      open={true}
      footer={null}
      onCancel={() => { skip(); return }}
      destroyOnClose
      maskClosable={false}
      width={560}
      className="tutorial-modal"
      styles={{ content: { padding: 0, overflow: 'hidden' } }}
    >
      <div className="tutorial-container">
        <h2 className="tutorial-title" style={{ fontFamily: 'Chakra Petch, sans-serif', fontWeight: 700 }}>Welcome to CloudAI Fusion Console</h2>
        <Steps current={current} className="tutorial-steps">
          {STEPS.map(s => (
            <Steps.Step key={s.title} title={s.title} />
          ))}
        </Steps>
        <div className="tutorial-body">
          <div className="tutorial-icon">✓</div>
          <div className="tutorial-content">
            <h3 className="step-title">{step.title}</h3>
            <div className="step-desc">{step.description}</div>
          </div>
        </div>
        <div className="tutorial-footer">
          <Button onClick={skip}>Skip</Button>
          <Space>
            {current > 0 && <Button onClick={prev} icon={<ArrowLeftOutlined />} disabled={current === 0}>Back</Button>}
            {current < STEPS.length - 1 ? (
              <Button type="primary" onClick={next}>{step.actionLabel}</Button>
            ) : (
              <Button type="primary" onClick={complete}>Finish</Button>
            )}
          </Space>
        </div>
      </div>
    </Modal>
  )
}
