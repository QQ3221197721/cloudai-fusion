import { useState, useRef } from 'react'
import { Card, Row, Col, Button, Typography, Space, Tag, Alert, Select, Divider, Input } from 'antd'
import { PlayCircleOutlined, StopOutlined, CodeOutlined } from '@ant-design/icons'

const { Title, Text } = Typography
const { Option } = Select

interface CodeInputProps {}

const SandboxRunner: React.FC<CodeInputProps> = () => {
  const [code, setCode] = useState('')
  const [language, setLanguage] = useState('rust')
  const [isRunning, setIsRunning] = useState(false)
  const [output, setOutput] = useState<string[]>([])
  const [memoryUsed, setMemoryUsed] = useState(0)
  const timeoutRef = useRef<number | null>(null)

  const defaultCode = `// 示例 WASM 代码 - Rust 语法
fn main() {
    println!("Hello from WASM sandbox!");
    
    let numbers = vec![1, 2, 3, 4, 5];
    let sum: i32 = numbers.iter().sum();
    
    println!("Sum: {}", sum);
}`

  const handleRun = async () => {
    if (!code.trim()) {
      alert('请输入代码')
      return
    }

    setIsRunning(true)
    setOutput(['编译中...', ...output.slice(0, -1)])
    
    // 模拟编译和执行过程
    await new Promise(resolve => setTimeout(resolve, 800))
    
    setOutput([...output.slice(1), '✓ 编译成功'])
    
    await new Promise(resolve => setTimeout(resolve, 600))
    
    setOutput([
      '执行环境已隔离',
      '内存限制：256MB',
      'CPU 限制：1 核',
      '超时时间：30s',
      '--- 输出 ---',
      'Hello from WASM sandbox!',
      'Sum: 15',
      '--- 完成 ---'
    ])
    
    setMemoryUsed(Math.random() * 50 + 20)
    setIsRunning(false)
  }

  const handleStop = () => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current)
    }
    setIsRunning(false)
    setOutput([...output, '✗ 执行已停止'])
  }

  const options = [
    { label: 'Rust', value: 'rust' },
    { label: 'Go', value: 'go' },
    { label: 'C/C++', value: 'cpp' },
    { label: 'Python', value: 'python' },
    { label: 'Wasmtime', value: 'wasmtime' }
  ]

  return (
    <div style={{ maxWidth: 1200, margin: '24px auto', padding: '0 24px' }}>
      <Title level={2} style={{ marginBottom: 8 }}>WASM 沙箱运行环境</Title>
      <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>
        在隔离环境中执行测试代码 · 自动安全边界 · 实时性能监控
      </Text>

      {/* Warning Banner */}
      <Alert
        type="warning"
        message="安全提示"
        description="此沙箱环境使用 WASM 运行时进行隔离执行。所有代码将在限制的资源环境下运行，禁止访问主机系统资源。"
        showIcon
        style={{ marginBottom: 24 }}
      />

      <Row gutter={[24, 24]}>
        {/* Editor Section */}
        <Col span={16}>
          <Card
            title={
              <Space>
                <CodeOutlined />
                <span>代码编辑器</span>
              </Space>
            }
            extra={
              <Space>
                <Select style={{ width: 120 }} value={language} onChange={setLanguage}>
                  {options.map(opt => <Option key={opt.value} value={opt.value}>{opt.label}</Option>)}
                </Select>
                <Button onClick={() => setCode(defaultCode)}>载入示例</Button>
              </Space>
            }
          >
            <Input.TextArea
              placeholder="在此输入或粘贴您的代码..."
              value={code}
              onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setCode(e.target.value)}
              rows={20}
              style={{ 
                fontFamily: '"Fira Code", "JetBrains Mono", monospace',
                fontSize: 13,
                lineHeight: 1.6,
                borderRadius: 8
              }}
            />

            <Divider />

            <Space size="large">
              <Button 
                type="primary" 
                icon={<PlayCircleOutlined />} 
                onClick={handleRun}
                disabled={isRunning || !code.trim()}
                size="large"
              >
                运行代码
              </Button>
              
              <Button 
                icon={<StopOutlined />} 
                onClick={handleStop}
                disabled={!isRunning}
                danger
                size="large"
              >
                停止
              </Button>

              <Text type="secondary">语言：<Tag color="blue">{language.toUpperCase()}</Tag></Text>
            </Space>
          </Card>
        </Col>

        {/* Output & Monitoring Section */}
        <Col span={8}>
          <Card
            title="执行状态"
            style={{ marginBottom: 24 }}
            bodyStyle={{ textAlign: 'center', padding: '40px 20px' }}
          >
            {isRunning ? (
              <>
                <div style={{ fontSize: 64, marginBottom: 16 }}>⚡</div>
                <Tag color="orange">运行中...</Tag>
                <Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
                  执行时限：{isRunning ? '30s' : '-'}
                </Text>
              </>
            ) : output.length > 0 ? (
              <>
                <div style={{ fontSize: 64, marginBottom: 16 }}>✓</div>
                <Tag color="green">已完成</Tag>
                <Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
                  内存使用：{Math.round(memoryUsed)}MB / 256MB
                </Text>
              </>
            ) : (
              <>
                <div style={{ fontSize: 64, marginBottom: 16 }}>⏸️</div>
                <Tag>待运行</Tag>
                <Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
                  准备就绪
                </Text>
              </>
            )}
          </Card>

          <Card title="控制台输出">
            <div style={{ 
              maxHeight: 300,
              overflowY: 'auto',
              background: '#1e1e1e',
              padding: 16,
              borderRadius: 8,
              fontFamily: 'monospace',
              fontSize: 12,
              lineHeight: 1.6,
              color: '#d4d4d4'
            }}>
              {output.length === 0 ? (
                <Text type="secondary">暂无输出</Text>
              ) : (
                <pre style={{ margin: 0 }}>{output.join('\n')}</pre>
              )}
            </div>
          </Card>
        </Col>
      </Row>

      {/* Security Configuration */}
      <Card style={{ marginTop: 24 }}>
        <Title level={5}>安全配置参数</Title>
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          <Col span={6}>
            <Text>内存限制：</Text>
            <Tag color="blue">256 MB</Tag>
          </Col>
          <Col span={6}>
            <Text>CPU 核心数：</Text>
            <Tag color="blue">1 核</Tag>
          </Col>
          <Col span={6}>
            <Text>执行超时：</Text>
            <Tag color="blue">30 秒</Tag>
          </Col>
          <Col span={6}>
            <Text>网络访问：</Text>
            <Tag color="red">禁用</Tag>
          </Col>
        </Row>
      </Card>
    </div>
  )
}

export default SandboxRunner
