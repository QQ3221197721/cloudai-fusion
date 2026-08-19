import { useState } from 'react'
import { Card, Row, Col, Steps, Button, Typography, Space, Tag, Alert, Statistic } from 'antd'
import { CheckCircleOutlined, DownloadOutlined } from '@ant-design/icons'

const { Title, Text, Paragraph } = Typography

const SetupWizard = () => {
  const [currentStep, setCurrentStep] = useState(0)
  const [isCompleted, setIsCompleted] = useState(false)

  const steps = [
    {
      title: '环境准备',
      description: '安装 OrbStack/Colima',
      content: (
        <div style={{ padding: '20px 0' }}>
          <Paragraph>
            CloudAI Fusion 推荐使用 OrbStack（macOS）或 Docker Desktop（Windows/Linux）作为本地容器运行时。
          </Paragraph>
          
          <Space direction="vertical" size={8}>
            <Tag color="blue">推荐：OrbStack (macOS)</Tag>
            <Paragraph style={{ fontSize: 13, marginBottom: 0 }}>
              • 更快的启动时间和更低的内存占用<br />
              • 一键安装：访问 <Text code>orbstack.dev</Text><br />
              • CLI: <Text code>brew install --cask orbstack</Text>
            </Paragraph>
            
            <Tag color="orange">备选：Docker Desktop</Tag>
            <Paragraph style={{ fontSize: 13, marginBottom: 0 }}>
              • 跨平台支持（macOS/Windows/Linux）<br />
              • 下载从 <Text code>docker.com/products/docker-desktop</Text><br />
              • 需要启用虚拟化技术（BIOS 设置）
            </Paragraph>
          </Space>

          <Button type="primary" icon={<DownloadOutlined />} style={{ marginTop: 16 }}>
            下载安装 OrbStack
          </Button>
        </div>
      )
    },
    {
      title: '系统验证',
      description: '检查硬件要求',
      content: (
        <div style={{ padding: '20px 0' }}>
          <Paragraph>
            确保您的开发机器满足以下最低要求：
          </Paragraph>

          <Card style={{ background: '#f0f5ff', border: '1px solid #d6e4ff' }}>
            <Row gutter={[16, 16]}>
              <Col span={8}>
                <Statistic title="内存" value={8} suffix="GB" />
                <Text type="secondary">建议 16GB+</Text>
              </Col>
              <Col span={8}>
                <Statistic title="CPU" value={4} suffix="核" />
                <Text type="secondary">建议 8 核 +</Text>
              </Col>
              <Col span={8}>
                <Statistic title="磁盘" value={50} suffix="GB" />
                <Text type="secondary">SSD 必需</Text>
              </Col>
            </Row>
          </Card>

          <Alert
            type="info"
            showIcon
            style={{ marginTop: 16 }}
            message="性能提示"
            description="对于 ML 训练任务，建议配置 GPU（MPS/MIG 支持的 GPU 卡）以获得最佳性能。"
          />
        </div>
      )
    },
    {
      title: '项目初始化',
      description: '克隆并配置仓库',
      content: (
        <div style={{ padding: '20px 0' }}>
          <Paragraph>
            执行以下步骤来设置本地开发环境：
          </Paragraph>

          <div style={{ 
            backgroundColor: '#1e1e1e', 
            borderRadius: 8, 
            padding: 16, 
            marginBottom: 16 
          }}>
            <pre style={{ margin: 0, color: '#d4d4d4', fontFamily: 'JetBrains Mono, monospace', fontSize: 13 }}>
{`# 1. 克隆仓库
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion

# 2. 安装依赖
npm install -g pnpm
pnpm install

# 3. 配置环境变量
cp .env.example .env
# 编辑 .env 文件填入您的 API 密钥

# 4. 启动开发服务器
pnpm dev`}
            </pre>
          </div>

          <Tag color="green">完成所有步骤后点击"下一步"</Tag>
        </div>
      )
    },
    {
      title: '启动服务',
      description: '验证运行状态',
      content: (
        <div style={{ padding: '20px 0' }}>
          <Paragraph>
            使用 Docker Compose 启动后端服务和数据库：
          </Paragraph>

          <div style={{ 
            backgroundColor: '#1e1e1e', 
            borderRadius: 8, 
            padding: 16, 
            marginBottom: 16 
          }}>
            <pre style={{ margin: 0, color: '#d4d4d4', fontFamily: 'JetBrains Mono, monospace', fontSize: 13 }}>
{`# 启动全部服务
docker-compose up -d

# 查看日志
docker-compose logs -f

# 测试连接
curl http://localhost:8080/health`}
            </pre>
          </div>

          <Space style={{ marginTop: 16 }}>
            <Button type="primary" onClick={() => setIsCompleted(true)}>
              我已验证服务运行正常
            </Button>
            <Button>查看错误日志</Button>
          </Space>
        </div>
      )
    }
  ]

  return (
    <div style={{ maxWidth: 900, margin: '40px auto', padding: '0 24px' }}>
      <Title level={2} style={{ textAlign: 'center', marginBottom: 8 }}>
        本地开发环境设置向导
      </Title>
      <Paragraph type="secondary" style={{ textAlign: 'center', marginBottom: 48 }}>
        按照这些步骤在你的本地机器上设置 CloudAI Fusion 开发环境
      </Paragraph>

      <Steps current={currentStep} items={steps.map(s => ({
        title: s.title,
        description: s.description,
      }))} style={{ marginBottom: 32 }} />

      <Card>
        {steps[currentStep].content}
      </Card>

      <div style={{ 
        display: 'flex', 
        justifyContent: 'space-between', 
        marginTop: 24,
        paddingTop: 24,
        borderTop: '1px solid #E2E8F0'
      }}>
        <Button 
          disabled={currentStep === 0} 
          onClick={() => setCurrentStep(currentStep - 1)}
        >
          上一步
        </Button>

        {isCompleted ? (
          <Button type="primary" ghost onClick={() => setCurrentStep(0)}>
            重新开始
          </Button>
        ) : currentStep < steps.length - 1 ? (
          <Button 
            type="primary" 
            onClick={() => setCurrentStep(currentStep + 1)}
          >
            下一步
          </Button>
        ) : (
          <Button 
            type="primary" 
            icon={<CheckCircleOutlined />}
            onClick={() => setIsCompleted(true)}
          >
            完成设置
          </Button>
        )}
      </div>
    </div>
  )
}

export default SetupWizard
