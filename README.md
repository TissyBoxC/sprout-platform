<div align="center">

<img src="assets/brand/sprout/brand_avatar.png" alt="如此萌屋" width="180" />

# 如此萌屋

### 芽系列 · 初芽

面向幼儿的模块化 AI 早教陪伴平台

[English](README.en.md) | 简体中文

[![Go](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Flutter](https://img.shields.io/badge/Flutter-Dart%203.11-02569B?logo=flutter&logoColor=white)](https://flutter.dev/)
[![Vue 3](https://img.shields.io/badge/Vue%203-TypeScript-4FC08D?logo=vuedotjs&logoColor=white)](https://vuejs.org/)
[![ESP-IDF](https://img.shields.io/badge/ESP--IDF-ESP32--S3-E7352C?logo=espressif&logoColor=white)](https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)

<!-- COMMUNITY_LINKS_START: 群链接待补充。 -->
<!-- DOCUMENTATION_LINKS_START: 项目文档入口待补充。 -->

</div>

`sprout-platform` 是“如此萌屋”首个产品“芽系列·初芽”的平台仓库，包含家长
控制端、后台管理端、业务服务、实时语音服务、共享契约和本地部署工具。固件和
AI 网关 fork 保持独立仓库，不在本仓库中重复版本化。

> 所有能力默认关闭或采用最保守配置。社交、摄像头、麦克风上传和数据采集必须由
> 监护人主动开启；儿童隐私、心理安全和人身安全优先于功能交付。

## 当前状态

仓库当前版本为 `0.15.0`，处于基础层录音、播放、唤醒、设备交互与内容库闭环阶段。功能状态以可运行的
端到端闭环为准，不以目录、接口或占位文件是否存在为准。

| 标记 | 含义 |
| --- | --- |
| `[已实现]` | 功能闭环、错误路径、权限或隐私约束和相关测试已经具备。 |
| `[部分实现]` | 已有可运行骨架、契约或单端能力，但尚未形成完整闭环。 |
| `[未实现]` | 尚未提供可验收的业务实现，仅有规划、目录或空接口。 |

## 全量功能清单

以下清单覆盖基础层、主流层和差异化层的 P0、P1、P2 范围。所有条目最终都应实现；
优先级只决定开发顺序，不代表可以删除功能。

### P0 必须能力

| ID | 功能 | 涉及模块 | 状态 | 当前说明 |
| --- | --- | --- | --- | --- |
| P0-01 | 系统启动、版本和错误恢复 | `system_core`、`module_registry`、`version_info`、`error_code`、`error_recovery`、`diagnostic_reporter` | `[已实现]` | 固件上报启动、模块失败和恢复事件；平台校验并幂等保存有界诊断历史；管理端可查看健康状态、启动历史、最近故障与恢复记录。 |
| P0-02 | 音频输入 | `audio_codec`、`audio_pipeline`、`voice_gateway` | `[已实现]` | 固件实现 16 kHz 单声道 Opus 编解码、I2S 采集、后置降噪/回声消除/增益校准与 VAD 分段，并支持扬声器参考回采；语音网关实现设备短期令牌鉴权、实时 WebSocket 音频会话、Opus 解码、环形缓冲、入站降噪/回声消除/增益校准、自适应 VAD 分段、ASR/LLM/TTS 适配器与对话闭环，以及语音控制帧契约和稳定错误码。 |
| P0-03 | 音频输出 | `audio_output`、`playback_queue`、`volume_control`、`prompt_tone`、`voice_gateway` | `[已实现]` | 固件实现按优先级的多路混音输出、监护人音量上限、静音保护安全提示音、本地提示音叠加和回采参考；语音网关把 TTS 回复接入按连接调度、支持抢占恢复、暂停、静音和清理的播放队列，并以稳定音频错误码上报混音失败。 |
| P0-04 | 语音唤醒 | `voice_wake`、`wake_feedback`、`voice_gateway` | `[已实现]` | 固件实现唤醒词检测、误唤醒抑制、唤醒反馈、本地提示音和 LED 状态；唤醒事件经脱敏心跳上报，语音网关接收并校验设备唤醒控制帧。 |
| P0-05 | 语音会话 | `voice_session`、`voice_gateway` | `[已实现]` | 设备用平台签发的短期令牌连接语音网关实时音频端点；唤醒词可直接建立监听会话，网关驱动 listening → thinking → speaking → listening 的连续多轮状态机并在每步下发 `session_state` 控制帧，播放中检测到儿童发言会立即清空当前回复回到监听（barge-in，安全提示音保留），空闲与时长超时结束会话；固件 `voice_session` 负责 WSS 传输、SRAW/SRSV 编解码、与 `audio_input`/`playback_queue` 的采集和播放联动。 |
| P0-06 | AI 对话 | `conversation_context`、`child_prompt_profile`、`voice_gateway`、`sub2api_fork` | `[已实现]` | 语音网关 `sub2api_client` 提供流式 OpenAI 兼容对话、重试与稳定错误映射，并按会话保留有界多轮上下文；`conversation_context` 在设备侧保存有界回合摘要与状态镜像，`child_prompt_profile` 持久化年龄层/内容等级并在会话建立时下发；`sub2api_fork` 在完成与模型接口上校验设备来源标签、模型白名单、配额与降级，并把脱敏调用统计写入审计。 |
| P0-07 | 内容 | 内容库、故事、儿歌、古诗、英语、百科、睡前、分龄模块 | `[已实现]` | 平台支持内容包草稿、提交审核、审核通过或驳回、发布、撤回、归档与审核历史；发布时校验内容文件 SHA-256 并原子自增目录版本，家长端按年龄层和分类增量同步、下载校验、缓存与撤回清理，管理端提供完整内容库操作界面，固件通过设备会话令牌拉取增量清单、断点续传下载、SHA-256 校验、SPIFFS 原子落盘并按环境音优先级播放。 |
| P0-08 | 联网 | `network_manager`、`device_provisioning`、`time_sync`、`cloud_auth`、`offline_fallback`、`provisioning_reporter`、`device_runtime_reporter` | `[已实现]` | 固件在 BLE 配网、设备绑定、时间同步、网络质量和云端鉴权之上新增 `provisioning_reporter`，把配网开始、无线网络配置、绑定、时间同步、断网重连和鉴权恢复等步骤写入有界事件缓冲并跨重启持久化，随心跳上报；`offline_fallback` 的待补传计数改为 NVS 持久化，且只在鉴权恢复并成功发送心跳后清零。平台新增配网事件审计表、`GET /api/v1/admin/devices/{device_id}/provisioning` 查询接口和 `POST /api/v1/admin/devices/{device_id}/sessions/revoke` 会话吊销接口，运行态接口回传当前配网状态、无线网络配置标记、会话状态和最近配网时间；家长端展示配网阶段进度、会话过期提示和断网空态，管理端提供配网时间线与吊销入口。 |
| P0-09 | 家长控制 | `parent_link`、`parent_policy`、`usage_report` | `[未实现]` | 未实现设备绑定、内容等级、使用时长、禁用时段、策略下发和使用报告。 |
| P0-10 | 安全与隐私 | `privacy_guard`、`content_filter`、`transport_security`、平台和语音服务安全模块 | `[部分实现]` | 已有 MQTT 双向 TLS、证书校验、请求标签、日志脱敏、安全默认值和契约校验；身份、权限、内容审核、数据删除和隐私授权闭环未完成。 |
| P0-11 | OTA | `ota_manager`、`ota_download`、`ota_validate`、`ota_rollback` | `[未实现]` | 未实现固件包管理、签名校验、灰度、下载、安装、回滚和版本统计。 |
| P0-12 | 设备交互 | 按键、LED、`prompt_tone`、`volume_control`、恢复出厂和状态反馈 | `[已实现]` | 实现按键手势、LED 状态、本地提示音、监护人音量上限、受保护的本机与远端恢复出厂、交互事件上报和幂等命令确认。 |

### P1 主流能力

| ID | 功能 | 涉及模块 | 状态 | 当前说明 |
| --- | --- | --- | --- | --- |
| P1-01 | 语音增强 | 全双工、远场拾音、连续会话 | `[未实现]` | 仅有 VAD 和音频基础目录，未完成双工、回声消除、远场和连续对话。 |
| P1-02 | 内容运营 | 内容包、主题包、更新、收藏、播放历史 | `[未实现]` | 未实现内容包生命周期、主题、增量更新、收藏和播放历史。 |
| P1-03 | 陪伴 | 情绪回应、鼓励、成长记录、习惯提醒、长记忆 | `[未实现]` | 未实现情绪上下文、成长记录、提醒、授权记忆和删除路径。 |
| P1-04 | 英语学习 | 单词、句型、口语练习、发音反馈 | `[未实现]` | 未实现课程、练习、评分、进度和家长端展示。 |
| P1-05 | 家长端 | 远程留言、点播、设备状态、内容推荐 | `[未实现]` | 家长端只有家庭页和设备列表骨架，尚无远程业务闭环。 |
| P1-06 | 本地兜底 | `offline_fallback`、离线故事、本地指令、缓存播放 | `[部分实现]` | 已实现断网宽限期与恢复判定；离线内容、本地指令、缓存播放和版本同步未完成。 |
| P1-07 | 诊断 | 系统诊断、日志、崩溃、网络质量、温度 | `[部分实现]` | 已实现固件启动、模块故障和恢复事件上报，平台脱敏访问日志与管理端诊断查询；崩溃转储、温度指标和长期聚合仍未完成。 |
| P1-08 | 摄像头 | 拍照、识物、绘本识别、拍照问答 | `[未实现]` | 未实现摄像头 capability、采集、本地处理、授权上传、模型调用和短期存储。 |

### P2 差异化能力

| ID | 功能 | 涉及模块 | 状态 | 当前说明 |
| --- | --- | --- | --- | --- |
| P2-01 | 屏幕 | 表情、动画、点读、视频播放 | `[未实现]` | 未实现显示驱动、界面、资源、点读和视频播放。 |
| P2-02 | 护眼屏幕 | 亮度、色温、距离和时长策略 | `[未实现]` | 未实现护眼策略、家长配置、提醒和设备执行。 |
| P2-03 | 触摸 | 点击、滑动、手势、触觉反馈 | `[未实现]` | 未实现触摸输入、手势识别和触觉反馈。 |
| P2-04 | 4G | eSIM、流量、远程唤醒、联网切换 | `[未实现]` | 未实现蜂窝模组、eSIM、流量限额、远程唤醒和网络切换。 |
| P2-05 | 电池 | 充电、电量估算、低功耗、深度休眠 | `[未实现]` | 未实现电量、充电、功耗策略、休眠和唤醒源。 |
| P2-06 | 动作 | 电机、舵机、行走、舞蹈、尾巴动作 | `[未实现]` | 未实现驱动、动作编排、停止保护和超时安全。 |
| P2-07 | 拟人形态 | 多外形能力集合 | `[未实现]` | 未实现按 capability 组合的形态 Profile；命名不绑定外观。 |
| P2-08 | 视频陪伴 | 视频通话、远程陪伴 | `[未实现]` | 未实现媒体协商、编解码、弱网、权限和通话记录。 |
| P2-09 | 穿戴与定位 | 定位、通话、电子围栏、SOS | `[未实现]` | 未实现定位、联系人、围栏、SOS、离线补报和紧急通知。 |
| P2-10 | 多设备 | 家庭联动、设备组网、内容同步 | `[未实现]` | 未实现消息路由、设备组、同步冲突处理和最终一致性。 |

### 分层覆盖

| 层级 | 范围 | 状态 | 说明 |
| --- | --- | --- | --- |
| 基础层 | 开机配网、唤醒、录音播放、AI 对话、内容、家长控制、OTA | `[部分实现]` | 录音、播放、唤醒、设备交互、AI 对话和内容库链路已经具备；家长控制和 OTA 闭环尚未实现。 |
| 主流层 | 连续会话、内容运营、陪伴、英语、远程留言、离线兜底、诊断、摄像头 | `[未实现]` | 除日志脱敏外尚无可用闭环。 |
| 差异化层 | 屏幕、触摸、护眼、4G、电池、动作、视频、定位、多设备 | `[未实现]` | 目录和规划不等同于实现，所有能力仍需按 capability 模块化落地。 |

### 补充模块

| 模块 | 用途 | 状态 | 当前说明 |
| --- | --- | --- | --- |
| `module_registry` | 注册、初始化和停止可选模块 | `[已实现]` | 固件支持可选模块注册、初始化失败记录和独立删除验证。 |
| `config_store` | 保存设备配置和能力集合 | `[已实现]` | 支持 NVS 字符串、二进制密钥读写和删除，供配网、绑定和音量持久化使用；生产镜像仍需启用 NVS 加密。 |
| `playback_queue` | 管理播放优先级、打断和恢复 | `[已实现]` | 固件与语音网关均实现按优先级排队、抢占、未播放尾音恢复、暂停和清理；安全提示音不可被打断或静音。 |
| `volume_control` | 执行监护人音量上限和本地静音 | `[已实现]` | 支持 0 到 100 的音量、策略上限钳制、持久化、静音以及 PCM 增益饱和。 |
| `prompt_tone` | 生成唤醒、采集、网络和安全的本地提示音 | `[已实现]` | 本地生成九类提示音，队列可用时按优先级播放，安全提示音保持最高优先级。 |
| `alarm_reminder` | 定时提醒、闹钟和日程 | `[未实现]` | 未实现时间同步、提醒调度和家长配置。 |
| `bluetooth_audio` | 蓝牙音箱模式和配对 | `[未实现]` | 未实现蓝牙音频、配对、模式切换和播放优先级。 |
| `learning_visualization` | 触屏学习内容和可视化反馈 | `[未实现]` | 未实现学习可视化、课程联动和屏幕交互。 |
| `long_term_memory` | 经监护人授权保存长期陪伴记忆 | `[未实现]` | 未实现授权、最小化存储、读取范围和删除。 |
| `image_privacy` | 图片本地处理和上传授权 | `[未实现]` | 未实现本地预处理、监护人授权、上传最小化和删除。 |
| `video_session` | 视频通话和远程陪伴媒体会话 | `[未实现]` | 未实现媒体协商、编解码、弱网和设备提示。 |

### 先行工程底座

以下内容不替代 P0/P1/P2 功能，但决定后续模块能否独立增删和回归。

| 能力 | 状态 | 当前说明 |
| --- | --- | --- |
| 共享契约与生成类型 | `[部分实现]` | 已有身份、家庭、儿童、设备、内容、OTA、策略、事件、MQTT、错误和界面文案 Schema，以及 Dart/TypeScript 生成类型；业务 API 契约尚未完整冻结。 |
| 契约校验和 CI | `[已实现]` | 平台 CI 校验本地部署、契约、生成类型、Go 服务、Vue 管理端和 Flutter 家长端。 |
| 自动 Release | `[已实现]` | `VERSION` 变更后创建中文 Release，并输出 Go 服务、管理端和契约产物；外部仓库独立发布。 |
| Docker Compose | `[已实现]` | 支持 PostgreSQL、Redis、MQTT/TLS、可选 `sub2api`、`device_platform` 和 `voice_gateway`。业务功能仍未完成。 |
| 请求标签、日志与脱敏 | `[已实现]` | 服务接入请求 ID、追踪字段、访问日志和敏感信息脱敏。 |
| 远程界面文案 | `[部分实现]` | 已有界面文案契约、平台模块骨架和管理端页面；发布、缓存、版本和固件消费闭环未完成。 |
| 品牌服务版本管理 | `[已实现]` | 管理端展示所有镜像的当前版本与仓库最新版本，可单独或批量升级平台服务与 AI 网关；自升级由独立执行器完成，管理端重启不中断任务。基础设施镜像只检测、不自动升级。 |
| `sub2api_fork` 定制 | `[未实现]` | fork 当前保留上游能力；租户标签、儿童策略、内部 API、调用审计和配置前缀尚未定制。 |

## 系统组成

| 项目 | 面向对象 | 主要职责 | 当前状态 |
| --- | --- | --- | --- |
| `apps/parent_app` | 家长和监护人 | 登录、家庭、儿童、设备、策略、报告、远程留言、内容和 OTA | `[部分实现]` |
| `apps/admin_web` | 平台运营和管理人员 | 账号权限、家庭儿童设备、内容、AI 网关、OTA、审计和监控 | `[部分实现]` |
| `services/device_platform` | 平台内部 | 家长与设备业务 API、设备通信、内容、策略、OTA、遥测和审计 | `[部分实现]` |
| `services/voice_gateway` | 平台内部 | 实时语音、ASR、TTS、内容安全、`sub2api` 调用和用量统计 | `[部分实现]` |
| `services/sub2api_fork` | AI 基础设施 | 模型账号池、路由、配额、限流、计费和供应商兼容 | `[部分实现]` |
| `firmware` | 游戏机本体 | 唤醒、录音、播放、会话、缓存、网络、策略和 OTA | `[部分实现]` |
| `packages/contracts` | 全项目共享 | HTTP、事件、MQTT、capability 和界面文案契约 | `[部分实现]` |

## 边界与调用关系

```text
家长 App ──────────────┐
                      ├──> device_platform ──> PostgreSQL / Redis / MQTT
后台 Web ──────────────┘

游戏机本体 ── MQTT/TLS ──> device_platform
     │
     └── WebSocket ─────> voice_gateway ──> sub2api ──> 模型供应商
                                  │
                                  ├──> ASR 服务
                                  └──> TTS 服务
```

- `parent_app` 只访问 `device_platform`，不直接连接设备。
- `admin_web` 只访问平台管理能力，不在浏览器中保存供应商密钥。
- `device_platform` 负责儿童、家庭、设备、内容、策略、OTA 和审计数据。
- `voice_gateway` 负责实时语音和 AI 调用适配，不保存家庭和设备业务主数据。
- `sub2api` 负责模型凭据、路由、配额和供应商，不包含儿童或设备数据。
- `firmware` 只保存设备身份、必要配置和缓存，不保存上游模型密钥。

## 工作区结构

项目采用混合式仓库布局：平台代码放在同一个仓库，固件和 AI 网关 fork 保持独立
仓库，并在平台工作区中以固定版本检出。

```text
sprout-platform/           当前平台仓库
  apps/
    parent_app/            Flutter 家长控制端
    admin_web/             Vue 3 后台管理端
  packages/
    contracts/             OpenAPI、事件、MQTT、capability 和文案契约
    go/                    共享 Go HTTP 和可观测性包
  services/
    device_platform/       Go 设备与家庭业务服务
    voice_gateway/         Go 实时语音和 AI 网关
  deploy/                  Docker Compose、PostgreSQL 初始化和 MQTT 配置
  tools/                   引导、契约、证书和本地部署工具
  workspace.lock.yaml      外部仓库版本锁定

sprout-firmware/           独立固件仓库，本地路径为 firmware/
sprout-sub2api-fork/       独立 fork 仓库，本地路径为 services/sub2api_fork/
```

`firmware/` 和 `services/sub2api_fork/` 由平台根目录 `.gitignore` 排除，分别在
各自仓库提交和推送。平台仓库不保存它们的源码副本。

## 公网端点

公网域名只在 `deploy/public-endpoints.env` 维护，其他文件和代码引用该配置：

| 用途 | 地址 |
| --- | --- |
| 家长端与设备 API | `https://api.clarkhub.cn` |
| 语音网关 | `https://voice.clarkhub.cn` |
| AI 网关与 Sub2API | `https://sub.clarkhub.cn` |
| 管理后台 | `https://admin.clarkhub.cn` |

设备 MQTT 域名尚未确认，确认前不要在生产环境生成证书。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 家长端 | Flutter、Dart、Riverpod、go_router、Dio、secure storage、json_serializable |
| 管理端 | Vue 3、TypeScript、Vite、Pinia、Vue Router、Axios |
| 业务服务 | Go、标准库 HTTP、pgx、go-redis、Eclipse Paho MQTT |
| 实时语音 | Go、WebSocket、Opus、ASR/TTS 适配器、`sub2api` |
| 数据与消息 | PostgreSQL 16、Redis 7、MQTT/TLS、MinIO AIStor 可选 |
| 固件 | ESP-IDF、C、FreeRTOS、PlatformIO、ESP32-S3 N16R8 |
| 契约 | JSON Schema、OpenAPI、Dart/TypeScript 生成类型 |
| 可观测性 | `log/slog`、请求 ID、追踪字段、访问日志和脱敏 |

## 环境准备

| 工具 | 建议版本 | 用途 |
| --- | --- | --- |
| Git | 当前稳定版 | 版本控制和外部仓库引导 |
| Flutter | Dart SDK 3.11.5 及以上 | 家长端开发和测试 |
| Node.js | 22.18 或 24.12 及以上 | 管理端、契约和部署校验 |
| Go | 1.27.1 及以上 | 两个平台服务 |
| Docker Desktop 或 Docker Engine | 当前稳定版，含 Compose v2 | 本地依赖和服务部署 |
| PlatformIO CLI | 当前稳定版 | 固件构建、烧录和测试 |

首次检出平台仓库后，在平台根目录执行：

```powershell
.\tools\bootstrap.ps1
```

缺少外部仓库时会按 `workspace.lock.yaml` 克隆；需要更新本地检出时：

```powershell
.\tools\bootstrap.ps1 -Update
```

`sub2api_fork` 同时保留 `origin` 和 `upstream`。`origin` 是可修改的
`sprout-sub2api-fork`，`upstream` 只用于同步上游，禁止向上游推送本项目业务。

## 构建与测试

### 共享契约

```powershell
Set-Location tools/contracts
npm ci
npm run validate
npm run generate
```

生成结果写入 `packages/contracts/generated` 和
`apps/parent_app/lib/core/contracts/generated`。提交前必须检查生成类型是否同步。

### 家长控制端

```powershell
Set-Location apps/parent_app
flutter pub get
flutter analyze
flutter test
flutter build apk --release
```

本地联调时需把服务地址编译进应用，例如：

```powershell
flutter run -d android --dart-define=API_BASE_URL=http://10.0.2.2:8081
```

家长端当前只维护 Android 和 iOS；Android 发布包会随平台 Release
以 `sprout-parent-app-vX.Y.Z.apk` 附件提供。桌面端和 Web 不属于交付范围。

如需重新生成应用图标，在 Flutter 环境可用时执行：

```powershell
dart run flutter_launcher_icons
```

### 后台管理端

```powershell
Set-Location apps/admin_web
npm ci
npm run type-check
npm run build
npm run test:e2e
```

首次运行端到端测试前执行：

```powershell
npx playwright install
```

构建产物位于 `apps/admin_web/dist`，该目录不进入 Git。

### Go 平台服务

```powershell
Set-Location services/device_platform
gofmt -w .
go test ./...
go build ./...

Set-Location ../voice_gateway
gofmt -w .
go test ./...
go build ./...
```

两个服务当前提供：

```text
GET /healthz
GET /readyz
```

`readyz` 当前返回服务就绪状态；数据库、Redis 和 MQTT 的独立依赖就绪判断仍待接入。

### 固件

```powershell
Set-Location firmware
platformio run -e esp32-s3-n16r8
```

固件所有构建中间文件、镜像和测试输出统一写入 `bgen/`。该目录不进入 Git。
`esp32-s3-n16r8-minimal` 用于验证关闭可选模块后仍能构建。

### 本地部署校验

```powershell
Set-Location tools/local-deployment
npm ci
npm run validate
```

## Docker 部署

### 1. 准备本地配置

首次部署在平台根目录执行：

```powershell
.\tools\local-deployment\Initialize-LocalEnvironment.ps1
```

脚本从 `deploy/.env.example` 生成 `deploy/.env`，并为数据库、Redis、管理员和
服务密钥生成随机值。`deploy/.env` 不得提交。

生成仅供本地开发使用的 MQTT 双向 TLS 证书：

```powershell
.\tools\generate-local-mqtt-certs.ps1
```

证书写入 `deploy/mosquitto/certs`。该目录已在 Git 中忽略，禁止把开发证书用于
生产环境。

### 2. 启动 Compose 服务

Compose 文件位于 `deploy/docker-compose.yml`，支持以下配置：

| 配置 | 启动内容 | 命令 |
| --- | --- | --- |
| 默认 | PostgreSQL、Redis、MQTT/TLS | `docker compose -f deploy/docker-compose.yml up -d` |
| AI | 默认服务加 `sub2api` | `docker compose -f deploy/docker-compose.yml --profile ai up -d` |
| 平台服务 | 默认服务加两个 Go 服务 | `docker compose -f deploy/docker-compose.yml --profile services up -d --build` |
| 全量 | 默认、AI 和平台服务 | `docker compose -f deploy/docker-compose.yml --profile ai --profile services up -d --build` |

平台和 AI 网关的发布与本地 Docker 必须同步。日常升级不需要手工拼 Compose
参数，在平台根目录执行：

```powershell
.\tools\synchronize-docker.ps1
```

脚本会从根目录 `VERSION` 和 `services/sub2api_fork/backend/cmd/server/VERSION`
读取已发布版本，构建 `sprout-local-*:<版本>` 镜像，重建应用容器并检查实际健康
接口。PostgreSQL、Redis、MQTT 和 AI 网关的数据卷保留，不会因升级被删除。

发布平台或 AI 网关版本时，CI 会同时推送同版本 GHCR 镜像：

| 组件 | GHCR 镜像 |
| --- | --- |
| 设备与家庭业务服务 | `ghcr.io/tissyboxc/sprout-device-platform` |
| 实时语音服务 | `ghcr.io/tissyboxc/sprout-voice-gateway` |
| 后台管理端 | `ghcr.io/tissyboxc/sprout-admin-web` |
| AI 网关 | `ghcr.io/tissyboxc/sub2api` |

任何一侧更新后都要核对另一侧：Docker 镜像标签必须对应已发布 tag，Release
必须包含对应容器镜像版本。

Docker Desktop 需要先启动并确认 Compose 可用：

```powershell
docker version
docker compose version
```

### 3. 检查运行状态

```powershell
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml logs -f device_platform
docker compose -f deploy/docker-compose.yml logs -f voice_gateway
docker compose -f deploy/docker-compose.yml logs -f sub2api
```

默认只监听本机地址：

| 服务 | 地址 |
| --- | --- |
| PostgreSQL | `127.0.0.1:5432` |
| Redis | `127.0.0.1:6379` |
| MQTT | `127.0.0.1:1883` |
| MQTT/TLS | `127.0.0.1:8883` |
| `sub2api` | `http://127.0.0.1:8080` |
| `device_platform` | `http://127.0.0.1:8081` |
| `voice_gateway` | `http://127.0.0.1:8082` |

健康检查：

```powershell
Invoke-RestMethod http://127.0.0.1:8081/healthz
Invoke-RestMethod http://127.0.0.1:8081/readyz
Invoke-RestMethod http://127.0.0.1:8082/healthz
Invoke-RestMethod http://127.0.0.1:8082/readyz
```

### 4. 停止和清理

停止服务但保留数据：

```powershell
docker compose -f deploy/docker-compose.yml down
```

同时删除本地 Compose 数据卷：

```powershell
docker compose -f deploy/docker-compose.yml down --volumes
```

删除数据卷会丢失本地数据库、Redis、MQTT 和 `sub2api` 数据，只能在确认不需要
恢复时执行。

## MinIO AIStor 可选部署

对象存储在需要保存合规的短期媒体、内容包或备份时启用。AIStor 不默认包含在
`deploy/docker-compose.yml` 中，因为它需要单独挂载许可证、证书和专用数据目录，
并且许可证和容量规划与业务服务不同。

AIStor 必须有有效许可证。免费层许可证可以在
[MinIO 定价页](https://min.io/pricing) 申请，免费层限制为单计算资源配置且不包含
商业支持。请先阅读许可证条款，再决定是否用于生产。

### Linux Docker

```bash
docker pull quay.io/minio/aistor/minio

mkdir -p "$HOME/minio/data" "$HOME/minio/certs"

# 将许可证文件保存为 $HOME/minio/minio.license

docker run -dt \
  -p 9000:9000 -p 9001:9001 \
  -v "$HOME/minio/data:/mnt/data" \
  -v "$HOME/minio/minio.license:/minio.license" \
  -v "$HOME/minio/certs:/etc/minio/certs" \
  --name "aistor-server" \
  quay.io/minio/aistor/minio:latest minio server /mnt/data \
  --license /minio.license

docker logs aistor-server
```

### Windows PowerShell

```powershell
$minioRoot = Join-Path $HOME "minio"
$minioData = Join-Path $minioRoot "data"
$minioCerts = Join-Path $minioRoot "certs"
New-Item -ItemType Directory -Force -Path $minioData, $minioCerts | Out-Null

# 将许可证文件保存为 $minioRoot\minio.license

docker pull quay.io/minio/aistor/minio
docker run -dt `
  -p 9000:9000 -p 9001:9001 `
  -v "${minioData}:/mnt/data" `
  -v "${minioRoot}\minio.license:/minio.license" `
  -v "${minioCerts}:/etc/minio/certs" `
  --name "aistor-server" `
  quay.io/minio/aistor/minio:latest minio server /mnt/data `
  --license /minio.license

docker logs aistor-server
```

### Podman

Podman 的目录和许可证准备方式相同，将 `docker pull` 和 `docker run` 替换为
`podman pull` 和 `podman run` 即可。容器配置、端口和卷挂载参数保持不变。

### 控制台和安全

- 控制台默认地址：`http://localhost:9001`。
- 如果首次初始化暂时使用本地默认账号，必须在开始使用前改成强密码。任何情况下
  都不得把默认密码带进共享或生产环境。
- 生产环境必须启用 TLS，使用正式证书，限制管理端访问来源，并遵循
  [MinIO 网络加密文档](https://docs.min.io/aistor/installation/container/network-encryption/)。
- `9000` 和 `9001` 不应直接暴露到公网。若必须跨主机访问，应放在受控反向代理、
  私网和访问控制之后。
- AIStor 当前是独立对象存储组件，平台服务尚未内置 AIStor 配置项；接入前必须先
  补充 Secret 注入、TLS 信任、租户隔离、保留期和删除策略。

### `mc` 客户端

Linux AMD64：

```bash
curl --progress-bar -L \
  https://dl.min.io/aistor/mc/release/linux-amd64/mc -o mc
chmod +x ./mc
sudo mv ./mc /usr/local/bin/
mc --version
```

Linux ARM64：

```bash
curl --progress-bar -L \
  https://dl.min.io/aistor/mc/release/linux-arm64/mc -o mc
chmod +x ./mc
sudo mv ./mc /usr/local/bin/
mc --version
```

## 本地服务配置

两个 Go 服务当前从环境变量加载配置，示例配置文件只用于说明部署形态。

### `device_platform`

```powershell
$env:DEVICE_PLATFORM_HTTP_HOST = "0.0.0.0"
$env:DEVICE_PLATFORM_HTTP_PORT = "8081"
$env:DEVICE_PLATFORM_DATABASE_DSN = "postgres://sprout:<password>@127.0.0.1:5432/sprout_device_platform?sslmode=disable"
$env:DEVICE_PLATFORM_REDIS_ADDRESS = "127.0.0.1:6379"
$env:DEVICE_PLATFORM_REDIS_PASSWORD = "<redis-password>"
$env:DEVICE_PLATFORM_MQTT_BROKER = "tls://127.0.0.1:8883"
$env:DEVICE_PLATFORM_MQTT_CA_FILE = "deploy/mosquitto/certs/ca.crt"
$env:DEVICE_PLATFORM_MQTT_CLIENT_CERTIFICATE_FILE = "deploy/mosquitto/certs/device.crt"
$env:DEVICE_PLATFORM_MQTT_CLIENT_KEY_FILE = "deploy/mosquitto/certs/device.key"
$env:DEVICE_PLATFORM_PHONE_VERIFICATION_MODE = "local"
$env:DEVICE_PLATFORM_ALLOW_LOCAL_SMS_BYPASS = "true"
```

### `voice_gateway`

```powershell
$env:VOICE_GATEWAY_HTTP_HOST = "0.0.0.0"
$env:VOICE_GATEWAY_HTTP_PORT = "8082"
$env:VOICE_GATEWAY_REDIS_ADDRESS = "127.0.0.1:6379"
$env:VOICE_GATEWAY_REDIS_PASSWORD = "<redis-password>"
$env:VOICE_GATEWAY_SUB2API_BASE_URL = "http://127.0.0.1:8080"
$env:VOICE_GATEWAY_SUB2API_API_KEY = "<local-api-key>"
$env:VOICE_GATEWAY_DATABASE_DSN = "postgres://sprout:<password>@127.0.0.1:5432/sprout_device_platform?sslmode=disable"
```

不要把真实密钥写入 README、源码、镜像、日志或前端配置。

## CI 与 Release

平台仓库包含：

- `.github/workflows/ci.yml`：校验部署文件、契约、生成类型、Go 服务、Vue 管理端
  和 Flutter 家长端。
- `.github/workflows/release.yml`：监听 `main` 分支上的 `VERSION` 变更，构建
  Go 服务、管理端、Android 安装包和契约归档，生成中文 Release 说明，并把
  发布文件同步到下载服务器后创建 GitHub Release 和对应镜像标签。

发布平台版本时只修改根目录 `VERSION`，格式固定为 `X.Y.Z`，提交并推送到 `main`。
标签由工作流自动生成和使用。`firmware`、`sprout-sub2api-fork` 在各自仓库独立
执行 CI 和 Release，不混入平台发布。

发布流水线通过 `SPROUT_RELEASE_UPLOAD_TOKEN` 调用管理端的内部发布接口，
将 APK、管理端包、契约包、服务二进制和校验文件写入下载服务器，并刷新
`index.json`。该令牌只能发布下载文件，不能读取家长账号、设备或 AI 数据。
GitHub 仓库和云端 `.env` 必须使用同一个令牌值。

## 模块化约束

- 每个功能都必须能独立增加和删除，删除只影响注册表、组合根、契约消费者和构建
  开关。
- 固件可选硬件使用独立 `CONFIG_FEATURE_<MODULE_NAME>` 开关，并在 CMake 中排除
  对应源码和依赖。
- 平台服务模块保持 `domain`、`service`、`handler`、`repository` 边界。
- 跨端协议进入 `packages/contracts`；服务内部传输契约留在各服务目录。
- 一个概念只使用一个 canonical 名称。设备形态使用 `camera`、`display`、
  `touch`、`cellular`、`battery` 等能力名，不使用角色外形命名。
- 公共 API 和复杂流程必须有简短、面向开发者的契约注释；注释解释约束和原因。

## 安全与隐私强制项

- 默认关闭或采用最保守配置；社交、摄像头、麦克风上传和数据采集必须由监护人
  明确开启。
- 所有公网通信使用 TLS 1.2+，必须校验证书，禁止跳过证书验证。
- 设备使用唯一身份和短期令牌，服务端密钥只存在于服务端 Secret 管理。
- 日志、指标、审计和错误响应不得包含完整对话、音频、图片、令牌、密钥或儿童
  个人信息。
- AI 请求和输出必须经过内容安全策略；模型供应商密钥不得下发设备或前端。
- 音频、图像、对话和诊断数据采用最短保留期，并提供授权撤回、删除和账号注销。
- 生产固件必须启用安全启动、Flash 加密、签名 OTA 和看门狗，并关闭调试接口。

## Git 工作流

提交信息统一使用：

```text
<type>(<scope>): <summary>
```

常用类型：

```text
feat
fix
refactor
perf
test
docs
build
chore
```

示例：

```text
feat(device_platform): add device activation flow
fix(voice_gateway): reject expired websocket sessions
docs(workspace): update external repository revisions
```

平台、固件和 `sub2api` fork 在各自仓库提交和推送。不提交 `AGENTS.md`、规划文档、
构建产物、本地配置、密钥、证书或无关文件。

## 开源组件

本项目保留第三方组件的版权和许可证声明，不重新授权第三方代码。主要依赖：

- [Go](https://go.dev/LICENSE)：BSD-3-Clause。
- [Flutter 和 Dart](https://github.com/flutter/flutter/blob/master/LICENSE)：BSD-3-Clause。
- [Vue 3](https://github.com/vuejs/core/blob/main/LICENSE)：MIT。
- [Vite](https://github.com/vitejs/vite/blob/main/LICENSE)：MIT。
- [Pinia](https://github.com/vuejs/pinia/blob/v3/LICENSE)：MIT。
- [Vue Router](https://github.com/vuejs/router/blob/main/LICENSE)：MIT。
- [Axios](https://github.com/axios/axios/blob/v1.x/LICENSE)：MIT。
- [Dio](https://github.com/cfug/dio/blob/main/dio/LICENSE)：MIT。
- [Riverpod](https://github.com/rrousselGit/riverpod/blob/master/LICENSE)：MIT。
- [go_router](https://github.com/flutter/packages/blob/main/packages/go_router/LICENSE)：BSD-3-Clause。
- [PostgreSQL](https://www.postgresql.org/about/licence/)：PostgreSQL License。
- [Redis](https://github.com/redis/redis/blob/unstable/LICENSE.txt)：AGPL-3.0，或按发行版使用 RSALv2/SSPLv1。
- [Eclipse Mosquitto](https://github.com/eclipse-mosquitto/mosquitto/blob/master/LICENSE.txt)：EPL-2.0。
- [ESP-IDF](https://github.com/espressif/esp-idf/blob/master/LICENSE)：Apache-2.0。
- [FreeRTOS Kernel](https://github.com/FreeRTOS/FreeRTOS-Kernel/blob/main/LICENSE.md)：MIT。
- [PlatformIO Core](https://github.com/platformio/platformio-core/blob/develop/LICENSE)：Apache-2.0。
- [MinIO AIStor](https://min.io/pricing)：使用前必须确认商业许可、免费层限制和支持范围。

平台仓库当前尚未声明项目级开源许可证；在添加 `LICENSE` 前，第一方代码不授予
额外使用许可。
