import { useState } from 'react'
import type { ReactNode } from 'react'
import { Card, Button, Upload, Space, Tag, Progress, Alert, Divider, Row, Col, Statistic } from 'antd'
import { UploadOutlined, CheckCircleOutlined, ExclamationCircleOutlined, ExperimentOutlined } from '@ant-design/icons'
import type { RcFile } from 'antd/es/upload'
import { validateEvidenceBundle } from '../lib/api'
import type { EvidenceValidationResult } from '../lib/api'
import './EvidenceVerify.css'

// A deliberately tampered 4-record chain used by the "Load broken chain example"
// button. Record #0 is a valid genesis, #1 links correctly, but #2's prev_hash
// does NOT match #1's hash — so validation must flag "record index 2".
const BROKEN_CHAIN_EXAMPLE = {
  key_id: 'demo-ed25519-key',
  run_mode: 'simulation',
  count: 4,
  checkpoint: { root_hash: 'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90', tree_size: 4, leaf_count: 4 },
  records: [
    { seq: 0, prev_hash: 'genesis', hash: 'aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888', signature: 'c2lnbmF0dXJlLXplcm8=', timestamp: '2026-08-17T09:00:00Z' },
    { seq: 1, prev_hash: 'aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888', hash: 'bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888cccc9999', signature: 'c2lnbmF0dXJlLW9uZQ==', timestamp: '2026-08-17T09:01:00Z' },
    { seq: 2, prev_hash: 'DEADBEEFtampered0000000000000000000000000000000000000000deadbeef', hash: 'cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888cccc9999dddd0000', signature: 'c2lnbmF0dXJlLXR3bw==', timestamp: '2026-08-17T09:02:00Z' },
    { seq: 3, prev_hash: 'cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888cccc9999dddd0000', hash: 'dddd4444eeee5555ffff6666aaaa7777bbbb8888cccc9999dddd0000eeee1111', signature: 'c2lnbmF0dXJlLXRocmVl', timestamp: '2026-08-17T09:03:00Z' },
  ],
}

export function EvidenceVerify(): JSX.Element {
  const [loading, setLoading] = useState(false)
  const [fileName, setFileName] = useState<string | null>(null)
  const [result, setResult] = useState<EvidenceValidationResult | null>(null)

  // runValidation is the single path both file uploads and the built-in example
  // flow through, so on-screen output is identical regardless of the source.
  const runValidation = async (json: unknown, sourceLabel: string): Promise<void> => {
    setLoading(true)
    setFileName(sourceLabel)
    setResult(null)
    try {
      const rep = await validateEvidenceBundle(json)
      setResult(rep)
    } catch (e) {
      setResult({
        success: false,
        error: `Failed to validate "${sourceLabel}": ${String(e)}.`,
        details: { count: 0, checkpointPresent: false, firstSeq: null, lastSeq: null, validSignatures: 0, invalidSignatures: 0 },
      })
    } finally {
      setLoading(false)
    }
  }

  // beforeUpload intercepts the file, reads it locally via the File API, validates
  // it, and returns false so antd never performs a network upload. File.text()
  // is used instead of FileReader because it returns a Promise and reliably
  // resolves the whole file across browsers.
  const handleFile = (file: RcFile): boolean => {
    void (async () => {
      setLoading(true)
      setFileName(file.name)
      setResult(null)
      try {
        const text = await file.text()
        const json = JSON.parse(text)
        await runValidation(json, file.name)
      } catch (e) {
        setResult({
          success: false,
          error: `Failed to parse "${file.name}": not valid JSON (${String(e)}).`,
          details: { count: 0, checkpointPresent: false, firstSeq: null, lastSeq: null, validSignatures: 0, invalidSignatures: 0 },
        })
        setLoading(false)
      }
    })()
    return false // prevent auto-upload
  }

  const loadBrokenExample = (): void => {
    void runValidation(BROKEN_CHAIN_EXAMPLE, 'broken-chain-example.json (built-in)')
  }

  return (
    <div style={{ padding: '20px 24px' }}>
      <Row gutter={[16, 16]} align="middle">
        <Col flex="auto">
          <h1 style={{ margin: 0, fontFamily: 'Chakra Petch, sans-serif', fontSize: '28px', fontWeight: 700 }}>Evidence Chain Validation</h1>
        </Col>
      </Row>
      <Alert
        message="Validate a local receipt / evidence chain export (.bundle.json or .chain.json)"
        description="The browser performs structural checks entirely offline: genesis link, hash-chain linkage (prev_hash consistency), monotonic sequence, base64 signature encoding, plus record counts and Merkle root extraction. Ed25519 signature math stays a backend responsibility."
        type="info"
        showIcon
        style={{ marginTop: 12 }}
      />

      <Card className="verify-card" style={{ marginTop: 16, boxShadow: '0 2px 8px rgba(0,0,0,0.15)' }} bodyStyle={{ padding: 24 }}>
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          <div className="drop-zone">
            <Upload.Dragger accept=".json" showUploadList={false} beforeUpload={handleFile} multiple={false} disabled={loading}>
              <p style={{ margin: 0 }}>
                <Button icon={<UploadOutlined />} size="large" loading={loading}>
                  Select Bundle File
                </Button>
              </p>
              <p className="hint">or drag &amp; drop a JSON evidence bundle here</p>
              {fileName && <p className="hint">Selected: <code>{fileName}</code></p>}
            </Upload.Dragger>
          </div>

          <div style={{ textAlign: 'center' }}>
            <Button
              icon={<ExperimentOutlined />}
              onClick={loadBrokenExample}
              disabled={loading}
              danger
            >
              Load broken chain example
            </Button>
            <p className="hint" style={{ marginTop: 6 }}>
              Loads a built-in 4-record chain whose 3rd record is tampered — use it to self-test broken-link detection.
            </p>
          </div>

          {result && (
            <>
              <Divider dashed />
              <ResultSummary result={result} />
              <ChainIntegrity result={result} />
            </>
          )}
        </Space>
      </Card>
    </div>
  )
}

// ResultSummary renders a high-level pass/fail banner + key metrics.
function ResultSummary({ result }: { result: EvidenceValidationResult }): JSX.Element {
  const d = result.details
  const brokenIdx = d.brokenChainIndex
  const title = result.success
    ? 'Chain verified ✅'
    : brokenIdx != null
      ? `Chain broken at record index ${brokenIdx} ❌`
      : 'Verification failed ❌'
  const tags: ReactNode[] = [
    d.keyId ? <Tag key="keyid" color="blue">{`Key ID: ${d.keyId.slice(0, 12)}…`}</Tag> : null,
    <Tag key="count" color="cyan">{`Records: ${d.count}`}</Tag>,
    d.checkpointPresent ? <Tag key="cp" color="green">Checkpoint present</Tag> : null,
    d.merkleRoot ? <Tag key="mr" color="purple">{`Merkle root: ${d.merkleRoot.slice(0, 20)}…`}</Tag> : null,
  ].filter(Boolean) as ReactNode[]

  return (
    <div className={`summary-box summary-${result.success ? 'ok' : 'fail'}`} role="status" aria-live="polite">
      <Space align="start">
        {result.success ? (
          <CheckCircleOutlined style={{ fontSize: 24, color: '#16c784' }} />
        ) : (
          <ExclamationCircleOutlined style={{ fontSize: 24, color: '#ff5555' }} />
        )}
        <div>
          <strong>{title}</strong>
          {result.success && d.merkleRoot && (
            <p className="ok-text">Records: {d.count} · Merkle root: <code>{d.merkleRoot}</code></p>
          )}
          {!result.success && result.error && <p className="error-text">{result.error}</p>}
          <div style={{ marginTop: 8 }}>{tags}</div>
          <Row gutter={[16, 16]} style={{ marginTop: 8 }}>
            {d.firstSeq != null && (
              <Col span={6}><Statistic title="First SEQ" value={d.firstSeq} /></Col>
            )}
            {d.lastSeq != null && (
              <Col span={6}><Statistic title="Last SEQ" value={d.lastSeq} /></Col>
            )}
            <Col span={10}>
              <Statistic
                title="Base64-valid signatures"
                value={`${d.validSignatures}/${d.validSignatures + d.invalidSignatures || d.count}`}
                valueStyle={{ fontFamily: 'IBM Plex Mono, monospace' }}
              />
            </Col>
          </Row>
        </div>
      </Space>
    </div>
  )
}

// ChainIntegrity visualizes hash-chain linkage status with an explicit index when
// the chain fails (no generic "failed" message).
function ChainIntegrity({ result }: { result: EvidenceValidationResult }): JSX.Element {
  const broken = result.details.brokenChainIndex
  if (broken == null || result.success) return <div className="timeline-section" />
  return (
    <div className="timeline-section">
      <h4 className="section-title">Chain Integrity</h4>
      <Progress percent={0} strokeColor="#ff5555" status="exception" />
      <p className="warning-text">
        Hash chain broke at record index <strong>#{broken}</strong>. Expected this record&apos;s
        <code> prev_hash</code> to equal the previous record&apos;s <code>hash</code>.
      </p>
    </div>
  )
}
