# 阿里云 T2 硬件验证「零遗漏」完整配置单

**生成时间**: 2026-08-20  
**方案**: 阿里云单平台按量付费  
**目标**: 验证 M2 MIG / M3 CRIU+RDMA / M5 SGX 三大 HARD 模块真实硬件能力  

---

## 1. 方案总览表

| 模块 | 实例规格族 | 精确型号 | 数量 | GPU 型号·数量·显存 | vCPU | 内存 GB | 是否支持 MIG/eRDMA/SGX | 预计时长 h | 单价 ¥/h (来源) | 小计 ¥ | 封顶预算 ¥ |
|------|-----------|---------|------|------------------|------|--------|----------------------|----------|---------------|------|----------|
| **M2** | gn7e (GPU 计算型) | ecs.gn7e-c16g1.4xlarge | 1 | NVIDIA A100×1 / 80GB | 16 | 125 | MIG✓ eRDMA✗ SGX✗ | 4h | 34.742 (来源 1,2) | 139 | 200 |
| **M3** | ebmgn7ex (GPU 裸金属) | ecs.ebmgn7ex.32xlarge | 2 | NVIDIA A100×8 / 80GB 每台 | 128 | 1024 | MIG✓ eRDMA✓160Gbps SGX✗ | 3h | ~278 (来源 3,估) ×2 = 556 | 1668 | 2500 |
| **M5** | g7t (安全增强) | ecs.g7t.xlarge | 1 | 无 GPU | 4 | 16 | MIG✗ eRDMA✗ SGX✓ (~8GB EPC) | 3h | ~1.5 (来源 4,估) | 4.5 | 50 |
| **总计** | — | — | **4 台** | — | — | — | — | — | — | **¥1811.5** | **¥4750** |

> ⚠️ **价格说明**:  
> - gn7e: 基于 developer.aliyun.com/article/1635082 和 zhuanlan.zhihu.com/p/629907226 确认 2024 年价格为 34.742 元/小时，2026 年可能调整 ±10%  
> - ebmgn7ex: 8×A100 裸金属，参考 gn7e 价格×8 + 裸金属溢价 (30-50%),估算每台约 278 元/h  
> - g7t: 通用型 g7.large 为 0.523 元/h(g7t 有 SGX 溢价),估算 xlarge 约 1.5 元/h  
> **实际下单前请以控制台实时报价为准**  

---

## 2. 每台实例的完整配置块

### M2 — NVIDIA MIG 验证 (ecs.gn7e-c16g1.4xlarge ×1)

#### 2.1 实例规格
- **规格族**: gn7e (GPU 计算型)
- **精确型号**: ecs.gn7e-c16g1.4xlarge
- **vCPU / 内存**: 16 vCPU / 125 GiB
- **GPU**: NVIDIA A100-SXM4 ×1 / 80GB GDDR6
- **是否支持 MIG**: ✅ 是 (nvidia-smi -mig 1)
- **是否支持 eRDMA**: ❌ 否 (仅 ebmgn7ex/gn8is 等裸金属支持)
- **SGX EPC**: ❌ N/A (非 SGX 实例)

#### 2.2 地域 + 可用区
- **推荐地域**: 华北 2(北京) cn-beijing / 华东 1(杭州) cn-hangzhou / 华北 3(张家口) cn-zhangjiakou
- **可用区**: 同一地域内任意一个可用区即可 (例如 cn-beijing-f, cn-hangzhou-g)

#### 2.3 镜像 OS + GPU 驱动 + CUDA
- **操作系统**: Ubuntu 22.04 LTS 64 位 或 Alibaba Cloud Linux 3.2104 LTS 64 位
- **GPU 驱动版本**: Driver 550+/CUDA 12.4+/cuDNN 9.2.0.82 (购买时勾选"安装 GPU 驱动"可自动安装)

#### 2.4 存储配置
- **系统盘**: ESSD AutoPL PL2 80 GB
- **数据盘**: 不需要

#### 2.5 网络配置
- **VPC**: 专有网络 (默认 VPC 或自定义，网段 192.168.0.0/16)
- **公网带宽**: 按使用流量计费，峰值 5 Mbps+EIP
- **带宽预估**: 5Mbps×2 天 ≈ 0.5GB 流量 ≈ 0.43 元

#### 2.6 安全组规则
- **入方向**: SSH TCP 22 / 来源 0.0.0.0/0
- **出方向**: 全部放通 (0.0.0.0/0)

#### 2.7 计费方式
- **单价**: ¥34.742/小时 (来源：developer.aliyun.com/article/1635082, zhuanlan.zhihu.com/p/629907226)
- **实例费用 (4 小时)**: 34.742×4 = ¥138.97
- **系统盘 (80GB×2 天)**: ¥1.92
- **公网流量**: ¥0.43
- **小计**: ¥141.32 ≈ ¥142

#### 2.8 特殊开启项
- **MIG 开启命令**:
  ```bash
  sudo nvidia-smi -i 0 --mig-enabled=1    # 优先尝试
  sudo nvidia-smi -i 0 -r                # 重置 GPU
  sudo reboot                            # 重启使驱动重新加载
  nvidia-smi --query-gpu=mig.mode.current --format=csv  # 应显示 Enabled
  ```
- **依赖文档**: https://help.aliyun.com/zh/ecs/user-guide/gpu-accelerated-compute-optimized-and-vgpu-accelerated-instance-families-1

#### 2.9 软件前置清单 (对齐 m2_mig_validation.sh)
```bash
sudo apt-get update && sudo apt-get upgrade -y
sudo apt-get install -y ubuntu-drivers-common git curl wget build-essential
sudo ubuntu-drivers autoinstall
curl -O https://go.dev/dl/go1.25.7.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.7.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
nvidia-smi
```

---

### M3 — CRIU + eRDMA 迁移验证 (ecs.ebmgn7ex.32xlarge ×2)

> **关键发现**: 阿里云 eRDMA 支持的 GPU 实例仅限:ebmgn7ex、ebmgn7ix、gn8is。gn7e **不支持 eRDMA**,故 M3 必须选用 ebmgn7ex 裸金属。  
> **来源**: https://help.aliyun.com/zh/ecs/user-guide/on-the-gpu-instance-configuration-erdma

#### 3.1 实例规格
- **规格族**: ebmgn7ex (GPU 弹性裸金属)
- **精确型号**: ecs.ebmgn7ex.32xlarge
- **vCPU / 内存**: 128 vCPU / 1024 GiB (每台)
- **GPU**: NVIDIA A100-SXM4 ×8 / 80GB ×8 = 640GB 总显存每台
- **MIG 支持**: ✅ 是
- **eRDMA 支持**: ✅ 是 (**唯一支持 eRDMA 的 A100 实例**)
  - **带宽**: 160 Gbit/s (两张 ERI 网卡各 80Gbps,绑定不同通道)

#### 3.2 地域 + 可用区
- **推荐地域**: 华北 2(北京) cn-beijing
- **可用区**: 两台必须同可用区 (如均为 cn-beijing-f),创建物理放置组确保在同一 rack
- **私网 IP**: 主机 10.0.1.20 + 备机 10.0.1.21 (示例)

#### 3.3 镜像 OS + GPU 驱动 + eRDMA 栈
- **操作系统**: Alibaba Cloud Linux 3.2104 LTS 64 位 (强烈推荐，自动安装 GPU 驱动 +eRDMA)
- **GPU/CUDA**: Driver 550.127.08 / CUDA 12.4.1 / cuDNN 9.2.0.82
- **eRDMA 软件栈**: MLNX_OFED_SRC-24.10-3.2.5.0,购买时勾选"安装 eRDMA 软件栈"✅
- **验证**: `eadm ver` → 应返回 kernel driver version 0.2.35

#### 3.4 存储配置
- **系统盘**: ESSD PL3 200 GB
- **数据盘**: 不需要

#### 3.5 网络配置
- **VPC**: 同一 VPC(网段 10.0.0.0/16,vSwitch 10.0.1.0/24)
- **公网带宽**: 按使用流量计费 10 Mbps×2EIP
- **安全组入方向**: SSH 22 / RDMA UDP 3283,4789 / 来源 10.0.1.0/24

#### 3.6 计费方式
- **单价**: ~¥278/小时/台 (估算:gn7e 基价 34.742×8×1.3 裸金属溢价)
- **实例费用 (3 小时×2 台)**: 278×3×2 = ¥1668
- **系统盘 (200GB×2 天×2)**: ¥5.76
- **公网流量**: ¥0.80
- **小计**: ¥1674.56 ≈ ¥1675

#### 3.7 特殊开启项
- **eRDMA 双节点组网**:
  ```bash
  ifconfig mlx5_0       # 查看 eRDMA 网卡
  ibstat                # 应显示 State:Active,Rate:100Gbps+
  ib_send_bw -d mlx5_0 10.0.1.21   # 跨节点带宽测试 ≥90Gbps
  ```
- **关键点**: 两台必须绑定到不同物理通道 (NetworkCardIndex=0 和 1),否则带宽减半

#### 3.8 软件前置清单 (对齐 m3_migration_validation.sh)
```bash
sudo yum groupinstall -y "Development Tools"
sudo yum install -y criu podman rsync ibutils ibverbs-providers git
eadm ver
ibstat
curl -O https://go.dev/dl/go1.25.7.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.7.linux-amd64.tar.gz
source ~/.bashrc
go version
# 运行前设置环境变量
export NODE_B_HOST=10.0.1.21
export NODE_B_SSH="root@10.0.1.21"
chmod +x m3_migration_validation.sh
./m3_migration_validation.sh
```

---

### M5 — Intel SGX Enclave 验证 (ecs.g7t.xlarge ×1)

#### 4.1 实例规格
- **规格族**: g7t (安全增强型)
- **精确型号**: ecs.g7t.xlarge
- **vCPU / 内存**: 4 vCPU / 16 GiB
- **加密内存 (EPC)**: 8 GiB (约 50% 内存作为 SGX EPC)
- **处理器**: Intel Ice Lake 第三代 Xeon®可扩展处理器
- **SGX 层级**: Intel SGX DCAP (Dynamic Attestation)
- **其他**: 无 GPU,不支持 MIG/eRDMA

#### 4.2 地域 + 可用区
- **推荐地域**: 华北 2(北京) cn-beijing / 华东 1(杭州) cn-hangzhou
- **可用区**: 任意可用区均可

#### 4.3 镜像 OS + SGX 驱动
- **操作系统**: Ubuntu 22.04 LTS 64 位
- **SGX 驱动**: in-kernel 驱动已包含 (Linux 5.11+),需安装用户态工具链
  ```bash
  sudo apt-get install -y libsgx-enclave-common libsgx-dcap-ql libsgx-urts sgx-aesm-service
  sudo systemctl start aesmd
  ```

#### 4.4 存储配置
- **系统盘**: ESSD PL2 40 GB
- **数据盘**: 不需要

#### 4.5 网络配置
- **VPC**: 默认 VPC(192.168.0.0/16)
- **公网带宽**: 按使用流量计费 5 Mbps+EIP

#### 4.6 安全组规则
- **入方向**: SSH TCP 22
- **出方向**: 全部放通

#### 4.7 计费方式
- **单价**: ~¥1.5/小时 (估算:g7.large 为 0.523 元/h,g7t 有 SGX 溢价)
- **实例费用 (3 小时)**: 1.5×3 = ¥4.50
- **系统盘 (40GB×2 天)**: ¥0.96
- **公网流量**: ¥0.43
- **小计**: ¥5.89 ≈ ¥6

#### 4.8 特殊开启项
- **SGX 设备节点验证**:
  ```bash
  ls -l /dev/sgx_enclave /dev/sgx_provision   # 应存在
  cpuid -1 | grep -i "SGX"                    # 应显示 Software Guard Extensions supported = true
  ```
- **Gramine 集成**:
  ```bash
  sudo curl -fsSLo /usr/share/keyrings/gramine-keyring.gpg \
    https://packages.gramineproject.io/gramine-keyring.gpg
  echo "deb [arch=amd64 signed-by=/usr/share/keyrings/gramine-keyring.gpg] \
    https://packages.gramineproject.io/ jammy main" | sudo tee /etc/apt/sources.list.d/gramine.list
  sudo apt-get update && sudo apt-get install -y gramine
  git clone https://github.com/gramineproject/gramine
  cd gramine/CI-Examples/helloworld && make SGX=1
  gramine-sgx-gen-private-key && gramine-sgx ./hello
  ```

#### 4.9 软件前置清单 (对齐 m5_sgx_validation.sh)
```bash
sudo apt-get update && sudo apt-get upgrade -y
sudo apt-get install -y libsgx-enclave-common libsgx-dcap-ql libsgx-urts sgx-aesm-service
sudo systemctl start aesmd
sudo apt-get install -y cpuid
curl -fsSLo /usr/share/keyrings/gramine-keyring.gpg https://packages.gramineproject.io/gramine-keyring.gpg
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/gramine-keyring.gpg] https://packages.gramineproject.io/ jammy main" | sudo tee /etc/apt/sources.list.d/gramine.list
sudo apt-get update && sudo apt-get install -y gramine
curl -O https://go.dev/dl/go1.25.7.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.7.linux-amd64.tar.gz
source ~/.bashrc
go version
ls -l /dev/sgx_enclave
chmod +x m5_sgx_validation.sh
./m5_sgx_validation.sh
```

---

## 5. 共享基础设施配置

### 5.1 阿里云账号
- **实名认证**: 企业实名 (开具增值税专票必要)
- **API 密钥**: RAM 子账号专属，策略 ecs:FullAccess + vpc:FullAccess

### 5.2 SSH Key Pair
- **密钥对名称**: cloudai-fusion-validation
- **生成**: ssh-keygen -t rsa -b 4096 -f cloudai-fusion-validation
- **授权**: 购买实例时选择该密钥对

### 5.3 VPC/vSwitch 规划
- **VPC 名称**: VPC-CloudAI-Fusion-Validation
- **VPC 网段**: 10.0.0.0/16
- **vSwitch 网段**: 10.0.1.0/24(可用区 A) + 10.0.2.0/24(可用区 B)

### 5.4 安全组模板
- **名称**: security-group-cloudai-validation
- **入站**: TCP 22(SSH),UDP 3283/4789(RDMA/InfiniBand,来源 10.0.0.0/16)
- **出站**: ALL,目标 0.0.0.0/0

---

## 6. 完整执行清单 (带 [] 勾选框)

### 第一阶段：准备工作 (Day-5 至 Day-1)
- [] 确认阿里云企业实名认证完成
- [] 申请 AccessKey(RAM 子账号)
- [] 生成 SSH 密钥对并上传至控制台
- [] 创建 VPC、vSwitch、安全组模板
- [] **提交 GPU 配额申请工单**(A100 需审批，路径:控制台→ECS→配额管理→申请增加配额，填写 gn7e-c16g1.4xlarge×1,ebmgn7ex.32xlarge×2,预计审批 1-3 工作日)
- [] 确认预算充值≥¥5000(预留冗余)

### 第二阶段：下单 (Day0)
- [] **购买 M2 实例**:华北 2(cn-beijing),规格 ecs.gn7e-c16g1.4xlarge,镜像 Ubuntu 22.04 LTS,存储 ESSD AutoPL 80GB,网络按流量计费 5Mbps+EIP,安全组开放 SSH 22,计费按量付费
- [] **购买 M3 实例×2**:华北 2(cn-beijing),同可用区 (cn-beijing-f),规格 ecs.ebmgn7ex.32xlarge×2,镜像 Alibaba Cloud Linux 3(勾选 eRDMA),存储 ESSD PL3 200GB,网络 eRDMA 双网卡 (主通道 0,辅通道 1),安全组 SSH+RDMA 3283/4789,计费按量付费
- [] **购买 M5 实例**:华北 2(cn-beijing),规格 ecs.g7t.xlarge,镜像 Ubuntu 22.04 LTS,存储 ESSD PL2 40GB,网络按流量计费 5Mbps+EIP,计费按量付费

### 第三阶段：开机 + 装环境 (Day0-1)
- [] **M2 实例**:启动实例→SSH 连接→执行 M2 环境安装脚本→nvidia-smi 显示 A100
- [] **M3 实例×2**:同时启动两台→分别 SSH 连接→执行 M3 环境安装脚本→ibstat 显示 Active 状态→互测 RDMA 带宽 ib_send_bw≥90Gbps
- [] **M5 实例**:启动实例→SSH 连接→执行 M5 环境安装脚本→ls/dev/sgx_enclave 存在→gramine-sgx hello 输出成功

### 第四阶段：运行验证脚本 (Day1)
- [] **M2 运行**:scp m2_mig_validation.sh root@<M2_EIP>:/tmp/;sudo/tmp/m2_mig_validation.sh 2>&1|tee m2_mig_result.log;grep "M2 MIG VALIDATION: PASSED" m2_mig_result.log;保存日志至本地归档
- [] **M3 运行**:两台均复制脚本;NODE_A 执行 sudo/tmp/m3_migration_validation.sh;NODE_B 被动等待 restore;grep "M3 MIGRATION VALIDATION: PASSED" m3_migration_result.log;保存端到端迁移时间
- [] **M5 运行**:scp m5_sgx_validation.sh root@<M5_EIP>:/tmp/;sudo/tmp/m5_sgx_validation.sh 2>&1|tee m5_sgx_result.log;grep "M5 SGX VALIDATION: PASSED" m5_sgx_result.log;保存 attestation quote 样本

### 第五阶段：关机 + 释放 (Day1)
- [] **M2 停机**:shutdown-h now;控制台停止实例 (不释放磁盘);释放 EIP
- [] **M3 停机**:两台均停机;释放 EIP×2;确认 RDMA 网卡解绑
- [] **M5 停机**:停机并释放 EIP
- [] **最终确认**:控制台→账单→费用明细→核对 3 笔实例费用;截图保存资源释放前后对比;归档日志文件至 docs/final-hardware-validation/results/

---

## 7. 费用汇总 + 开票步骤

### 7.1 总费用概览
| 项目 | 单价¥/h | 数量 | 时长 h | 小计¥ |
|------|---------|------|-------|------|
| M2(gn7e-c16g1.4xlarge) | 34.742 | 1 | 4 | 139 |
| M3(ebmgn7ex.32xlarge) | 278 | 2 | 3 | 1668 |
| M5(g7t.xlarge) | 1.5 | 1 | 3 | 4.5 |
| 系统盘 (共 4 台) | — | — | — | 9 |
| 公网流量 (4EIP) | — | — | — | 2 |
| **总计** | — | — | — | **¥1822.5** |
| **封顶预算** | — | — | — | **¥4750** |

> 💡 **预算余量**: ¥4750 - ¥1822.5 = ¥2927.5(冗余 160%,覆盖超时/重试)

### 7.2 增值税专票开具步骤
- **路径**: 控制台→费用中心→发票管理→申请发票
- **所需信息**: 发票抬头公司全称、税号、单位地址电话、开户行及账号、邮箱
- **税率**: 6%(增值税专用发票可抵扣)
- **类型**: 电子专用发票
- **到账时间**: 申请后 1-3 个工作日发送至邮箱
- **凭证留存**: 将发票 PDF 存档至 financial/invoices/2026-08-hardware-validation.pdf

---

## 8. 诚实提示 (必阅)

### 8.1 价格不确定性
- ⚠️ **当前价格是 2026-08-20 调研价**,阿里云会不定期调价
- **核实路径**: ECS 控制台→实例与镜像→实例规格详情→立即购买→按量付费→查看实时报价
- **建议**: 下单前务必截图确认价格并附到本文档作为变更基准

### 8.2 A100 配额风险
- ⚠️ **A100 实例非随时可买**,尤其在热门地域 (北京、上海、硅谷)
- **配额申请时长**: 正常 1-3 工作日，大促期间可能延长至 5-7 日
- **备选方案**: 如 gn7e 缺货，可尝试 gn7i-c16g1.4xlarge(A10) 降级，但性能下降 30-50%

### 8.3 MIG/eRDMA/SGX启用注意
- **MIG 重启**: 启用 MIG 后必须重启实例 (sudo reboot),且重启期间无法 SSH 访问约 2-3 分钟
- **eRDMA 通道绑定**: 两台 M3 实例必须绑定到不同通道 (Index 0 vs Index 1),否则带宽仅 80Gbps 而非 160Gbps
- **SGX BIOS**: 极少数宿主机 BIOS 未开启 SGX,如遇问题请提工单更换宿主机
- **远程 Attestation**: DCAP 远程证明需要访问外网 PCCS 服务，如网络受限只能做本地证明

### 8.4 时间窗口建议
- **M2**: 4 小时足够 (实际验证通常<1 小时),留出 3 小时缓冲应对重启/故障
- **M3**: 3 小时 (需两台协调，易出现 SSH 延迟/网络抖动)
- **M5**: 3 小时 (主要耗时在 Gramine 编译/SDK 下载)
- **总时长**: 建议 Day1 全天预留，最晚不超过当日 24 点 (避免次日凌晨计费周期跨天混乱)

---

## 9. 数据来源 URL 列表 (标日期 2026-08-20)

| 编号 | 内容 | URL | 备注 |
|------|------|-----|------|
| [1] | gn7e A100 实例规格 | https://www.foreignserver.com/aliyun/gn7e.html | 参数+vCPU+GPU+网络 |
| [2] | gn7e-c16g1.4xlarge 价格 | https://developer.aliyun.com/article/1635082 | 2024 年报价 34.742 元/h |
| [3] | ebmgn7ex裸金属+eRDMA | https://help.aliyun.com/zh/egs/gpu-accelerated-compute-optimized-instance-families | 160Gbps eRDMA+MIG支持 |
| [4] | g7t SGX 实例规格 | https://help.aliyun.com/zh/ecs/user-guide/enhanced-instance-families/ | 50%EPC+Ice Lake 处理器 |
| [5] | eRDMA 配置步骤 | https://help.aliyun.com/zh/ecs/user-guide/on-the-gpu-instance-configuration-erdma | 双通道绑定详解 |
| [6] | MIG 开启指南 | https://help.aliyun.com/zh/ecs/user-guide/gpu-accelerated-compute-optimized-and-vgpu-accelerated-instance-families-1 | nvidia-smi-mig-1 |
| [7] | CRIU 安装文档 | https://criu.org/Installation | 官方安装指南 |
| [8] | Gramine SGX SDK | https://gramine.readthedocs.io/en/stable/installation.html | Gramine 安装 |

---

**文档结束**  
**最后修订**: 2026-08-20 15:30 CST  
**责任人**: 清源项目组·徐梓涵  
**联系方式**: xxx@email.com  
**下次复核**: 2026-08-27(一周后价格可能调整，需重新核对)
