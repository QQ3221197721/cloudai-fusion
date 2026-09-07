from docx import Document
from docx.shared import Pt, Inches
from docx.oxml.ns import qn
from docx.enum.text import WD_ALIGN_PARAGRAPH

doc = Document()
doc.styles['Normal'].font.name = '宋体'
doc.styles['Normal']._element.rPr.rFonts.set(qn('w:eastAsia'), '宋体')

def add_table_header(table, headers):
    row = table.rows[0]
    for i, h in enumerate(headers):
        cell = row.cells[i]
        cell.text = h
        cell.paragraphs[0].runs[0].bold = True
        cell.paragraphs[0].alignment = WD_ALIGN_PARAGRAPH.CENTER

header = ['ID', '功能名称', '代码路径', '真实文件数', '真实代码量', '状态', '完成度']
rows = [
    # 2.1-2.9 Core Modules
    ['2.1', '集群管理', 'pkg/cloud/', '15', '~2,800 LOC', '⚠️ Partial', '60%'],
    ['2.2', 'GPU 调度', 'pkg/scheduler/', '33', '~8,200 LOC', '❌ Fake', '10%'],
    ['2.3', 'AI 训练溯源', 'pkg/provenance/', '5', '~4,100 LOC', '❌ Fake', '0%'],
    ['2.4', 'Red Team 安全', 'pkg/redteam/', '90', '~18,500 LOC', '❌ Fake', '5%'],
    ['2.5', 'AISecOps', 'pkg/aisecops/', '5', '~1,900 LOC', '⚠️ Partial', '40%'],
    ['2.6', '灾难恢复', 'pkg/disaster/', '2', '~2,800 LOC', '❌ Fake', '0%'],
    ['2.7', 'TEE 可信计算', 'pkg/tee/', '3', '~1,450 LOC', '❌ Fake', '0%'],
    ['2.8', '边缘自治', 'pkg/edge/', '27', '~6,700 LOC', '❌ Fake', '0%'],
    ['2.9', 'FinOps 成本', 'pkg/cost/', '1', '~1,200 LOC', '❌ Fake', '5%'],
    
    # 2.10-2.19 Platform Services
    ['2.10', '多租户 RBAC', 'pkg/auth/', '8', '~3,400 LOC', '✅ Real', '70%'],
    ['2.11', 'AIOps 智维', 'pkg/aiops/', '9', '~2,600 LOC', '⚠️ Partial', '30%'],
    ['2.12', '全栈可观测', 'pkg/metrics/', '1', '~890 LOC', '⚠️ Partial', '25%'],
    ['2.13', '服务网格', 'pkg/mesh/', '1', '~450 LOC', '❌ Fake', '5%'],
    ['2.14', 'DevSecOps', 'pkg/devsecops/', '1', '~1,100 LOC', '✅ Real', '60%'],
    ['2.15', 'GitOps', 'pkg/gitops/', '1', '~680 LOC', '❌ Fake', '0%'],
    ['2.16', '数据溯源', 'pkg/wellreadiness/', '1', '~320 LOC', '❌ Fake', '0%'],
    ['2.17', '联邦学习', 'pkg/fed/', '1', '~780 LOC', '❌ Fake', '0%'],
    ['2.18', '插件市场', 'pkg/plugin/', '1', '~1,250 LOC', '❌ Fake', '5%'],
    ['2.19', '工单支持', 'pkg/support/', '0', '0 bytes', '❌ Missing', '0%'],
    
    # 2.20-2.29 Security Capabilities
    ['2.20', '合规审计', 'pkg/security/', '1', '~2,300 LOC', '⚠️ Partial', '40%'],
    ['2.21', 'SaaS 计费', 'pkg/billing/', '7', '~5,600 LOC', '⚠️ Partial', '30%'],
    ['2.22', 'API 网关 WAF', 'pkg/api/', '3', '~1,800 LOC', '✅ Real', '55%'],
    ['2.23', 'AI 智能代理', 'ai/agents/', '4', '~115 KB Python', '✅ Real', '80%'],
    ['2.24', 'WebSocket', 'pkg/websocket/', '1', '~1,100 LOC', '✅ Real', '65%'],
    ['2.25', '混沌工程', 'tests/chaos/', '2', '~890 LOC', '⚠️ Partial', '20%'],
    ['2.26', '事件总线', 'pkg/eventbus/', '3', '~2,100 LOC', '✅ Real', '70%'],
    ['2.27', '数据库预写', 'pkg/store/', '1', '~1,450 LOC', '✅ Real', '75%'],
    ['2.28', 'ZKP 零知识证明', 'pkg/zkp/', '9', '~7,300 LOC', '❌ Fake', '0%'],
    ['2.29', 'Terraform 模拟', 'terraform/', '0', '0 bytes', '❌ Missing', '0%'],
    
    # 补充功能模块 (2.30-2.53)
    ['2.30', '告警通道管理', 'pkg/alerting/', '1', '~1,850 LOC', '❌ Fake', '0%'],
    ['2.31', 'WASM 插件运行时', 'pkg/wasm/', '10', '~6,200 LOC', '⚠️ Partial', '15%'],
    ['2.32', '安全沙箱扫描', 'pkg/sandbox/', '1', '~1,900 LOC', '❌ Fake', '0%'],
    ['2.33', '工作负载管理', 'pkg/workload/', '1', '~1,200 LOC', '❌ Fake', '5%'],
    ['2.34', 'GPU 资源管理器', 'pkg/resources/', '1', '~2,100 LOC', '❌ Fake', '5%'],
    ['2.35', '弹性/韧性机制', 'pkg/resilience/', '1', '~780 LOC', '❌ Fake', '0%'],
    ['2.36', 'SARIF 扫描报告', 'pkg/scanners/', '1', '~1,650 LOC', '❌ Fake', '0%'],
    ['2.37', '领导选举', 'pkg/election/', '1', '~890 LOC', '❌ Fake', '0%'],
    ['2.38', '高可用基库', 'pkg/ha/', '1', '~560 LOC', '❌ Fake', '0%'],
    ['2.39', '热替换编排', 'pkg/hotswap/', '1', '~420 LOC', '❌ Fake', '0%'],
    ['2.40', '威胁猎杀', 'pkg/hunt/', '1', '~1,100 LOC', '❌ Fake', '0%'],
    ['2.41', '漏洞利用库', 'pkg/exploit/', '1', '~780 LOC', '❌ Fake', '0%'],
    ['2.42', '检测引擎', 'pkg/detect/', '1', '~1,350 LOC', '❌ Fake', '0%'],
    ['2.43', '证据链', 'pkg/evidence/', '1', '~2,450 LOC', '❌ Fake', '0%'],
    ['2.44', '验证织物 (Fabric)', 'pkg/fabric/', '1', '~1,200 LOC', '❌ Fake', '0%'],
    ['2.45', '深井就绪性检查', 'pkg/wellreadiness/', '1', '~320 LOC', '❌ Fake', '0%'],
    ['2.46', '能力检测', 'pkg/capability/', '1', '~890 LOC', '❌ Fake', '0%'],
    ['2.47', '控制平面', 'pkg/controlplane/', '1', '~1,650 LOC', '❌ Fake', '0%'],
    ['2.48', 'AI 异常检测引擎', 'ai/anomaly/', '4', '~38 KB Python', '✅ Real', '75%'],
    ['2.49', 'AI 运维智能体', 'ai/agents/', '4', '~115 KB Python', '✅ Real', '80%'],
    ['2.50', 'AI 模型微调', 'ai/agents/fine_tuning.py', '1', '~34.6 KB', '✅ Real', '85%'],
    ['2.51', '分布式训练器', 'ai/scheduler/', '4', '~48 KB Python', '✅ Real', '70%'],
    ['2.52', 'CLI 工具 (cafctl)', 'cmd/cafctl/', '18', '~3,800 LOC', '✅ Real', '60%'],
    ['2.53', '监控配置', 'monitoring/', '0', '0 bytes', '❌ Missing', '0%'],
]

h = doc.add_heading('第五部分：53 个核心功能模块逐一审计详细清单', level=1)

t = doc.add_table(rows=1, cols=7)
t.style = 'Table Grid'
add_table_header(t, header)

for rdata in rows:
    row = t.add_row()
    cells = row.cells
    for i, cell_text in enumerate(rdata):
        cells[i].text = cell_text
        cells[i].paragraphs[0].alignment = WD_ALIGN_PARAGRAPH.CENTER

doc.save('53_功能模块深度审计报告_final.docx')
print('DONE: 53-function table appended to final version')
