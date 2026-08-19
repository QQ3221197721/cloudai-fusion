import { useState } from 'react'
import { Card, Collapse, Typography, Space, Alert, Button, Tag, List } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'

const { Title, Text } = Typography
const { Panel } = Collapse

const troubleshootGuide = [
  {
    question: 'Docker Desktop 无法启动怎么办？',
    answer: (
      <div>
        <p><strong>原因分析：</strong>虚拟化技术未启用或 Hyper-V 冲突</p>
        <p><strong>解决方案：</strong></p>
        <ol>
          <li>重启计算机进入 BIOS，启用 Intel VT-x 或 AMD-V 虚拟化</li>
          <li>对于 Windows 10/11，确保已启用"适用于 Linux 的 Windows 子系统"和"虚拟机平台"</li>
          <li>运行 PowerShell（管理员）：<Text code>Enable-WindowsOptionalFeature -FeatureName Microsoft-Hyper-V -All</Text></li>
          <li>重启后重新尝试启动 Docker</li>
        </ol>
      </div>
    ),
    category: 'docker'
  },
  {
    question: 'OrbStack 启动后容器网络不通？',
    answer: (
      <div>
        <p><strong>原因分析：</strong>DNS 解析问题或端口冲突</p>
        <p><strong>解决方案：</strong></p>
        <ol>
          <li>检查端口占用：命令行执行 <Text code>lsof -i:8080</Text></li>
          <li>清除 DNS 缓存：<Text code>sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder</Text></li>
          <li>在 OrbStack 设置中重启网络适配器</li>
          <li>如果仍失败，修改 <Text code>.env</Text>中的 API 端口号</li>
        </ol>
      </div>
    ),
    category: 'orbstack'
  },
  {
    question: '容器权限拒绝访问文件系统？',
    answer: (
      <div>
        <p><strong>原因分析：</strong>Docker 共享目录配置问题</p>
        <p><strong>解决方案：</strong></p>
        <ol>
          <li>Docker Desktop → Preferences → Resources → File Sharing</li>
          <li>添加您的工作目录路径（如 <Text code>~/projects/cloudai-fusion</Text>）</li>
          <li>重启 Docker 服务使更改生效</li>
          <li>Linux 用户可检查用户组权限：<Text code>sudo usermod -aG docker $USER</Text></li>
        </ol>
      </div>
    ),
    category: 'permissions'
  },
  {
    question: 'pnpm install 依赖安装失败？',
    answer: (
      <div>
        <p><strong>原因分析：</strong>镜像源网络问题或 Node 版本不兼容</p>
        <p><strong>解决方案：</strong></p>
        <ol>
          <li>更换为国内镜像源：</li>
          <pre style={{ background: '#f5f5f5', padding: 8, fontFamily: 'monospace' }}>
{`pnpm config set registry https://registry.npmmirror.org`}
          </pre>
          <li>清理缓存后重试：<Text code>pnpm store prune && pnpm install</Text></li>
          <li>确认 Node.js 版本 ≥ 18.0（使用 nvm: <Text code>nvm use 18</Text>）</li>
        </ol>
      </div>
    ),
    category: 'networking'
  },
  {
    question: '浏览器访问 localhost:3000 空白页？',
    answer: (
      <div>
        <p><strong>原因分析：</strong>Vite 开发服务器未正常启动</p>
        <p><strong>解决方案：</strong></p>
        <ol>
          <li>检查终端日志是否有错误输出</li>
          <li>关闭其他占用 3000 端口的进程：<Text code>lsof -ti:3000 | xargs kill -9</Text></li>
          <li>删除 <Text code>node_modules/.vite</Text>缓存目录后重试</li>
          <li>尝试不同的端口：<Text code>VITE_PORT=8080 pnpm dev</Text></li>
        </ol>
      </div>
    ),
    category: 'orbstack'
  },
  {
    question: '.env 文件变量未生效？',
    answer: (
      <div>
        <p><strong>原因分析：</strong>Vite 环境变量命名规则问题</p>
        <p><strong>解决方案：</strong></p>
        <ol>
          <li>Vite 仅加载以 <Text code>VITE_</Text>前缀的环境变量</li>
          <li>例如应改为：<Text code>VITE_API_BASE_URL=http://localhost:8080/api/v1</Text></li>
          <li>重启开发服务器以读取新的环境变量</li>
          <li>不要在代码中直接引用非 VITE 前缀的敏感信息</li>
        </ol>
      </div>
    ),
    category: 'permissions'
  }
]

const TroubleshootingGuide = () => {
  const [selectedCategory, setSelectedCategory] = useState<string>('all')

  const categories = [
    { key: 'all', label: '全部问题', count: troubleshootGuide.length },
    { key: 'orbstack', label: 'OrbStack', count: troubleshootGuide.filter(f => f.category === 'orbstack').length },
    { key: 'docker', label: 'Docker', count: troubleshootGuide.filter(f => f.category === 'docker').length },
    { key: 'networking', label: '网络问题', count: troubleshootGuide.filter(f => f.category === 'networking').length },
    { key: 'permissions', label: '权限问题', count: troubleshootGuide.filter(f => f.category === 'permissions').length }
  ]

  const filteredFAQs = selectedCategory === 'all' 
    ? troubleshootGuide 
    : troubleshootGuide.filter(f => f.category === selectedCategory)

  return (
    <div style={{ maxWidth: 1000, margin: '40px auto', padding: '0 24px' }}>
      <Title level={2}>故障排除指南</Title>
      <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>
        解决本地开发环境中常见的问题和错误
      </Text>

      <Card style={{ marginBottom: 24 }}>
        <Space wrap>
          {categories.map(cat => (
            <Tag
              key={cat.key}
              color={selectedCategory === cat.key ? 'blue' : 'default'}
              style={{ cursor: 'pointer', padding: 8 }}
              onClick={() => setSelectedCategory(cat.key)}
            >
              {cat.label} ({cat.count})
            </Tag>
          ))}
        </Space>
      </Card>

      <Alert
        type="info"
        message="快速提示"
        description="如果以下方案不能解决问题，请查看项目 GitHub Issues 页面或联系技术支持。"
        showIcon
        style={{ marginBottom: 24 }}
      />

      <Collapse defaultActiveKey={['0']} expandIconPosition="end">
        {filteredFAQs.map((item, index) => (
          <Panel
            header={
              <Space>
                <QuestionCircleOutlined style={{ color: '#4C8DFF' }} />
                <Text strong>{item.question}</Text>
              </Space>
            }
            key={index}
          >
            <div style={{ padding: '8px 0' }}>{item.answer}</div>
          </Panel>
        ))}
      </Collapse>

      <List
        style={{ marginTop: 24 }}
        dataSource={['查看详细文档', '提交新问题', '联系技术支持']}
        renderItem={(item) => (
          <List.Item>
            <Button type="link" style={{ padding: 0 }}>{item}</Button>
          </List.Item>
        )}
      />
    </div>
  )
}

export default TroubleshootingGuide
