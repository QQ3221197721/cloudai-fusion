import { RunModeBadge } from './RunModeBadge'
import { MockDataBanner } from './MockDataBanner'
import type { RunMode } from '../types'
import './Header.css'

interface Props {
  mode: RunMode
  isMockSource?: boolean
  mockReason?: string
}

// Header is the always-resident top bar. It surfaces the run-mode badge on
// every screen and, when data came from a mock fallback, a loud disclosure
// banner directly beneath it so the operator is never misled.
export function Header({ mode, isMockSource = false, mockReason }: Props): JSX.Element {
  const tooltipText =
    mode === 'production' ? 'All subsystems backed by real external services.' :
    mode === 'degraded' ? 'Degraded mode — some subsystems fall back to simulated backends (surfaced).' :
    'Simulation mode — backends replaced with in-memory emulators.'

  return (
    <>
      <header className="header-container">
        <div className="header-logo">
          <svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg">
            <rect width="32" height="32" rx="6" fill="#0a0e0f" />
            <path d="M8 20 L14 8 L18 16 L22 10 L26 24" fill="none" stroke="#2fd4d4" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
            <circle cx="14" cy="8" r="1.8" fill="#16c784" />
            <circle cx="22" cy="10" r="1.8" fill="#f5a623" />
          </svg>
          <span>CloudAI Fusion · Console</span>
        </div>
        <nav className="header-nav">
          <RunModeBadge mode={mode} />
          <span className="mode-tooltip">{tooltipText}</span>
        </nav>
      </header>
      <MockDataBanner
        visible={isMockSource}
        message={
          mockReason
            ? `Data displayed is MOCK — ${mockReason} Endpoint /api/v1/capabilities was unavailable.`
            : undefined
        }
      />
    </>
  )
}
