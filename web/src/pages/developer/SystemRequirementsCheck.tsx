import { useState } from 'react'
import { Card, Row, Col, Typography, Space, Tag, Statistic, Progress, Table, Alert, Button } from 'antd'
import { CheckCircleOutlined, ExclamationCircleOutlined } from '@ant-design/icons'

const { Title, Text, Paragraph } = Typography

interface HardwareInfo {
  ram: { total: number; usable: number }
  cpuCores: number
  diskAvailable: number
}

const mockHardwareInfo: HardwareInfo = {
  ram: { total: 16384, usable: 16000 },
  cpuCores: 8,
  diskAvailable: 120
}

const SystemRequirementsCheck = () => {
  const [scanning, setScanning] = useState(false)
  const [scanComplete, setScanComplete] = useState(false)

  const requirements = [
    {
      name: '内存 (RAM)',
      required: '8 GB',
      recommended: '16 GB+',
      current: `${mockHardwareInfo.ram.usable / 1024} GB`,
      pass: mockHardwareInfo.ram.usable >= 8 * 1024
    },
    {
      name: 'CPU 核心数',
      required: '4 核',
      recommended: '8 核 +',
      current: `${mockHardwareInfo.cpuCores} 核`,
      pass: mockHardwareInfo.cpuCores >= 4
    },
    {
      name: '磁盘空间',
      required: '50 GB',
      recommended: '100 GB+',
      current: `${mockHardwareInfo.diskAvailable} GB`,
      pass: mockHardwareInfo.diskAvailable >= 50
    },
    {
      name: '虚拟化支持',
      required: '开启',
      recommended: 'Intel VT-x/AMD-V',
      current: '检测中...',
      pass: true
    }
  ]

  const handleScan = () => {
    setScanning(true)
    setTimeout(() => {
      setScanning(false)
      setScanComplete(true)
    }, 1500)
  }

  const columns = [
    {
      title: '硬件资源',
      dataIndex: 'name',
      key: 'name',
      render: (text: string) => <Text strong>{text}</Text>
    },
    {
      title: '要求',
      dataIndex: 'required',
      key: 'required',
      render: (text: string) => <Tag color="red">{text}</Tag>
    },
    {
      title: '建议',
      dataIndex: 'recommended',
      key: 'recommended',
      render: (text: string) => <Tag color="blue">{text}</Tag>
    },
    {
      title: '当前状态',
      dataIndex: 'current',
      key: 'current',
      render: (text: string) => <Text code style={{ fontSize: 14 }}>{text}</Text>
    },
    {
      title: '状态',
      key: 'status',
      render: (_: any, record: any) => (
        <Space>
          {record.pass ? (
            <Tag icon={<CheckCircleOutlined />} color="green">通过</Tag>
          ) : (
            <Tag icon={<ExclamationCircleOutlined />} color="error">未满足</Tag>
          )}
        </Space>
      )
    }
  ]

  return (
    <div style={{ maxWidth: 1000, margin: '40px auto', padding: '0 24px' }}>
      <Title level={2}>系统要求检查</Title>
      <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>
        验证您的开发环境是否满足 CloudAI Fusion 的最低运行要求
      </Text>

      {!scanComplete && (
        <Card style={{ textAlign: 'center', padding: 40, marginBottom: 24 }}>
          <Text style={{ fontSize: 48, display: 'block', marginBottom: 16 }}>⚙️</Text>
          <Paragraph>
            点击下方按钮开始系统硬件扫描
          </Paragraph>
          <Button 
            type="primary" 
            size="large" 
            onClick={handleScan}
            loading={scanning}
          >
            开始检测
          </Button>
        </Card>
      )}

      {scanComplete && (
        <>
          <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
            <Col span={8}>
              <Card>
                <Statistic 
                  title="检查结果" 
                  value={requirements.filter(r => r.pass).length}
                  suffix={`/${requirements.length}`}
                  prefix={<CheckCircleOutlined />}
                />
              </Card>
            </Col>
            <Col span={8}>
              <Card>
                <Statistic 
                  title="总计项目" 
                  value={requirements.length}
                />
              </Card>
            </Col>
            <Col span={8}>
              <Card>
                <Progress 
                  percent={(requirements.filter(r => r.pass).length / requirements.length) * 100}
                  strokeColor="#52c41a"
                  format={() => '符合要求'}
                />
              </Card>
            </Col>
          </Row>

          <Alert
            message="检测结果"
            description={
              requirements.every(r => r.pass)
                ? '恭喜！您的系统满足所有要求，可以开始开发工作了。'
                : '部分系统要求未满足。请按照建议升级硬件或调整配置后再继续。'
            }
            type={requirements.every(r => r.pass) ? 'success' : 'warning'}
            icon={<CheckCircleOutlined />}
            showIcon
            style={{ marginBottom: 24 }}
          />

          <Table
            columns={columns}
            dataSource={requirements}
            rowKey="name"
            pagination={false}
            size="small"
          />

          <Card style={{ marginTop: 24 }}>
            <Title level={5}>下一步</Title>
            <Paragraph type="secondary">
              如果您通过了所有检查，可以继续设置本地开发环境。如果有任何项目失败，请参考下方的故障排除指南。
            </Paragraph>
            <Space>
              <Button type="primary">前往故障排除</Button>
              <Button>查看详情说明</Button>
            </Space>
          </Card>
        </>
      )}
    </div>
  )
}

export default SystemRequirementsCheck
