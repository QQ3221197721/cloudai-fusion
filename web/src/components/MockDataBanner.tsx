import { Alert } from 'antd'
import './MockDataBanner.css'

interface Props {
  visible: boolean
  message?: string
}

export function MockDataBanner({ visible, message = 'Data displayed is MOCK — backend API unreachable. Endpoints like /api/v1/capabilities were not available at runtime.' }: Props): JSX.Element {
  if (!visible) return <></>

  return (
    <div className="mock-data-banner">
      <Alert message={message} type="warning" showIcon closable />
    </div>
  )
}
