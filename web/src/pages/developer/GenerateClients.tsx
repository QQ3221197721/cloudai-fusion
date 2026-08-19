import { useState } from 'react'
import { Card, Row, Col, Upload, Button, Select, Typography, Space, Tag, message, Tabs, Progress, Input } from 'antd'
import {
  UploadOutlined, CodeOutlined, FileTextOutlined,
  CheckCircleOutlined, DownloadOutlined, InfoCircleOutlined
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'

const { Title, Text, Paragraph } = Typography
const { Dragger } = Upload

interface CodeSnippetProps {
  language: string
}

const CodeSnippet: React.FC<CodeSnippetProps> = ({ language }) => {
  const examples: Record<string, string> = {
    typescript: `import { CloudAIApiClient } from '@cloudai-fusion/sdk'

const client = new CloudAIApiClient({
  apiKey: process.env.CLOUDAI_API_KEY,
  endpoint: 'https://api.cloudai-fusion.com/v1'
})

async function main() {
  const redteam = await client.redteam.createEngagement({
    target: 'localhost',
    duration: '24h'
  })
  
  console.log('Engagement:', redteam)
}`,
    go: `package main

import (
  "fmt"
  "github.com/cloudai-fusion/cloudai-fusion-go/client"
)

func main() {
  c := client.New("https://api.cloudai-fusion.com/v1", 
    "your-api-key")
  
  engagement, err := c.RedTeam.CreateEngagement(
    &client.CreateEngagementRequest{
      Target:   "localhost",
      Duration: "24h",
    },
  )
  if err != nil {
    panic(err)
  }
  
  fmt.Printf("Engagement created: %s\\n", engagement.ID)
}`,
    python: `from cloudai_fusion import CloudAIClient

client = CloudAIClient(
    api_key="your-api-key",
    base_url="https://api.cloudai-fusion.com/v1"
)

engagement = client.redteam.create_engagement(
    target="localhost",
    duration="24h"
)

print(f"Engagement created: {engagement.id}")`
  }
  
  const code = examples[language as keyof typeof examples] || examples.typescript
  
  return (
    <div style={{
      position: 'relative',
      backgroundColor: '#1e1e1e',
      borderRadius: 8,
      overflow: 'hidden',
      marginBottom: 16
    }}>
      <div style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        padding: '12px 16px',
        backgroundColor: '#252526',
        borderBottom: '1px solid #3c3c3c'
      }}>
        <Tag icon={<CodeOutlined />} color="blue">
          {language === 'typescript' ? 'TypeScript' : language === 'go' ? 'Go' : 'Python'}
        </Tag>
        <Button
          size="small"
          icon={<DownloadOutlined />}
          onClick={() => {
            const blob = new Blob([code], { type: 'text/plain' })
            const url = URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = `example.${language}`
            a.click()
            URL.revokeObjectURL(url)
            message.success('代码片段已下载')
          }}
        >
          下载示例
        </Button>
      </div>
      <pre style={{
        margin: 0,
        padding: 20,
        overflowX: 'auto',
        fontSize: 13,
        lineHeight: 1.6,
        color: '#d4d4d4',
        fontFamily: '"Fira Code", "JetBrains Mono", monospace'
      }}>
        <code>{code}</code>
      </pre>
    </div>
  )
}

interface UsageGuideProps {}

const UsageGuide: React.FC<UsageGuideProps> = () => {
  const steps = [
    {
      title: '安装 SDK',
      content: '根据您的开发语言选择合适的 SDK 包：\n\n• TypeScript: npm install @cloudai-fusion/sdk\n• Go: go get github.com/cloudai-fusion/cloudai-fusion-go\n• Python: pip install cloudai-fusion',
      icon: <CheckCircleOutlined />
    },
    {
      title: '配置凭据',
      content: '在环境变量中设置 CLOUDAI_API_KEY，或在代码中直接传入 API Key\n\n切勿将 API Key 提交到版本控制系统！',
      icon: <InfoCircleOutlined />
    },
    {
      title: '初始化客户端',
      content: '使用您的 API Key 初始化客户端实例，指定正确的 API 端点地址',
      icon: <InfoCircleOutlined />
    },
    {
      title: '调用 API',
      content: '参考代码示例调用所需的 API 方法，处理异步响应和错误情况',
      icon: <InfoCircleOutlined />
    }
  ]

  return (
    <Card
      title={
        <Space>
          <FileTextOutlined style={{ color: '#4C8DFF' }} />
          <span>API 使用指南</span>
        </Space>
      }
      style={{ marginTop: 24 }}
    >
      <Row gutter={[16, 16]}>
        {steps.map((step, index) => (
          <Col span={12} key={index}>
            <div style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 12,
              paddingBottom: 16,
              borderBottom: index < steps.length - 1 ? '1px solid #E2E8F0' : 'none'
            }}>
              <div style={{
                flexShrink: 0,
                width: 32,
                height: 32,
                borderRadius: '50%',
                backgroundColor: '#E0F2FE',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: '#0284C7'
              }}>
                {step.icon}
              </div>
              <div style={{ flex: 1 }}>
                <Title level={5} style={{ margin: '0 0 8px 0' }}>{step.title}</Title>
                <Paragraph style={{ margin: 0, fontSize: 13 }}>
                  {step.content}
                </Paragraph>
              </div>
            </div>
          </Col>
        ))}
      </Row>
    </Card>
  )
}

const GenerateClients = () => {
  const navigate = useNavigate()
  const [selectedLanguage, setSelectedLanguage] = useState('typescript')
  const [fileList, setFileList] = useState<any[]>([])
  const [generating, setGenerating] = useState(false)
  const [progress, setProgress] = useState(0)

  const handleGenerate = async () => {
    if (fileList.length === 0) {
      message.warning('请先上传 OpenAPI 规范文件')
      return
    }

    setGenerating(true)
    setProgress(0)

    const interval = setInterval(() => {
      setProgress(prev => {
        if (prev >= 100) {
          clearInterval(interval)
          return 100
        }
        return prev + 10
      })
    }, 100)

    setTimeout(() => {
      clearInterval(interval)
      setGenerating(false)
      message.success('API 客户端生成完成！')
    }, 1500)
  }

  return (
    <div style={{
      maxWidth: 1200,
      margin: '0 auto',
      padding: 24,
      minHeight: 'calc(100vh - 64px)'
    }}>
      {/* Page Header */}
      <div style={{ marginBottom: 32 }}>
        <Title level={2} style={{ margin: 0 }}>API 客户端生成器</Title>
        <Text type="secondary">从 OpenAPI 规范自动生成跨语言的 API 客户端代码</Text>
      </div>

      {/* Main Content */}
      <Row gutter={[24, 24]}>
        <Col span={16}>
          <Card style={{ marginBottom: 24 }}>
            <Dragger
              accept=".json,.yaml,.yml"
              fileList={fileList}
              onChange={info => setFileList(info.fileList)}
              onRemove={() => setFileList([])}
              multiple={false}
              maxCount={1}
              customRequest={({ onSuccess }) => {
                setTimeout(() => onSuccess?.('ok'), 1000)
                setFileList([{ status: 'done', name: 'openapi-spec.json' }])
              }}
            >
              <p className="ant-upload-text">
                <UploadOutlined style={{ fontSize: 32, color: '#4C8DFF' }} />
              </p>
              <p className="ant-upload-hint">
                拖拽 OpenAPI 规范文件（JSON/YAML）到这里<br />
                或点击上传以导入您的 API 定义
              </p>
            </Dragger>
          </Card>

          <Card style={{ marginBottom: 24 }}>
            <Title level={5}>生成选项</Title>
            <Space style={{ marginTop: 16, flexWrap: 'wrap' }}>
              <div style={{ marginRight: 24 }}>
                <Text style={{ display: 'block', marginBottom: 8 }}>目标语言：</Text>
                <Select
                  value={selectedLanguage}
                  onChange={setSelectedLanguage}
                  options={[
                    { label: 'TypeScript/JavaScript', value: 'typescript' },
                    { label: 'Go', value: 'go' },
                    { label: 'Python', value: 'python' },
                    { label: 'Rust', value: 'rust' },
                    { label: 'Java', value: 'java' },
                  ]}
                  style={{ width: 200 }}
                  prefix={<CodeOutlined />}
                />
              </div>
              <div>
                <Text style={{ display: 'block', marginBottom: 8 }}>命名空间：</Text>
                <Input placeholder="例如：cloudai.api" style={{ width: 200 }} />
              </div>
              <div>
                <Text style={{ display: 'block', marginBottom: 8 }}>生成类型：</Text>
                <Select
                  defaultValue="full"
                  options={[
                    { label: '完整客户端', value: 'full' },
                    { label: '仅类型定义', value: 'types-only' },
                    { label: '简化版客户端', value: 'lightweight' }
                  ]}
                  style={{ width: 200 }}
                />
              </div>
            </Space>

            <Space style={{ marginTop: 16 }}>
              <Button
                type="primary"
                icon={<CodeOutlined />}
                size="large"
                onClick={handleGenerate}
                loading={generating}
              >
                生成 API 客户端
              </Button>
              <Button size="large" onClick={() => navigate('/developer/api-clients/download')}>
                查看生成的文件
              </Button>
            </Space>
          </Card>

          {generating && (
            <Card style={{ marginTop: 16 }}>
              <Title level={5}>生成进度</Title>
              <Progress percent={progress} strokeColor="#4C8DFF" style={{ marginTop: 12 }} />
              <Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
                正在生成 {selectedLanguage} 客户端...
              </Text>
            </Card>
          )}
        </Col>

        <Col span={8}>
          <Card
            title={
              <Space>
                <FileTextOutlined style={{ color: '#4C8DFF' }} />
                <span>快速开始</span>
              </Space>
            }
            style={{ marginBottom: 24 }}
          >
            <ol style={{ margin: 0, paddingLeft: 16 }}>
              <li style={{ marginBottom: 12 }}>上传 OpenAPI 规范文件</li>
              <li style={{ marginBottom: 12 }}>选择目标语言和生成选项</li>
              <li style={{ marginBottom: 12 }}>点击“生成 API 客户端”</li>
              <li>下载并使用生成的客户端代码</li>
            </ol>
          </Card>

          <Card title="支持的语言">
            <Space direction="vertical" style={{ width: '100%' }}>
              <Tag color="blue">TypeScript / JavaScript</Tag>
              <Tag color="orange">Go</Tag>
              <Tag color="green">Python</Tag>
              <Tag color="red">Rust</Tag>
              <Tag color="purple">Java</Tag>
            </Space>
          </Card>
        </Col>
      </Row>

      {/* Code Snippets Section */}
      <Tabs
        defaultActiveKey="typescript"
        items={[
          {
            key: 'typescript',
            label: 'TypeScript',
            children: <CodeSnippet language="typescript" />
          },
          {
            key: 'go',
            label: 'Go',
            children: <CodeSnippet language="go" />
          },
          {
            key: 'python',
            label: 'Python',
            children: <CodeSnippet language="python" />
          }
        ]}
        style={{ marginTop: 32 }}
      />

      {/* Usage Guide */}
      <UsageGuide />
    </div>
  )
}

export default GenerateClients
