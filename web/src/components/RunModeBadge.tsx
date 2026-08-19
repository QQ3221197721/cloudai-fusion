import './Header.css'
import type { RunMode } from '../types'

interface Props {
  mode: RunMode
}

// RunModeBadge renders the always-on run-mode indicator. Accessibility rule:
// never rely on color alone — every state carries an explicit text label in
// brackets so color-blind users and no-color terminals can still read it.
export function RunModeBadge({ mode }: Props): JSX.Element {
  const config: Record<RunMode, { label: string; color: string; bg: string; border: string }> = {
    production: { label: '[PRODUCTION]', color: '#16c784', bg: 'rgba(22,199,132,0.12)', border: '#16c784' },
    degraded: { label: '[DEGRADED]', color: '#f5a623', bg: 'rgba(245,166,35,0.12)', border: '#f5a623' },
    simulation: { label: '[SIMULATION MODE]', color: '#ff5555', bg: 'rgba(255,85,85,0.12)', border: '#ff5555' },
  }
  const c = config[mode]

  return (
    <span
      className="run-mode-badge"
      role="status"
      aria-label={`Run mode: ${c.label}`}
      style={{ backgroundColor: c.bg, borderColor: c.border, color: c.color }}
    >
      <span className="run-mode-dot" style={{ backgroundColor: c.color }} aria-hidden="true" />
      {c.label}
    </span>
  )
}
