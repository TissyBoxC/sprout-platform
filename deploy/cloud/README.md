# 如此萌屋云端部署（1Panel）

这是一套与本地开发栈分离的云端部署包。云端只拉取 GitHub Release 发布的
镜像，不在服务器上编译 Go、Flutter 或 Vue。

## 1. 目录结构

```text
deploy/cloud/
├── docker-compose.yml
├── .env.example
├── ../public-endpoints.env   # 公网域名唯一配置源
├── download/
│   ├── nginx/default.conf      # 只读下载入口，不开启目录索引
│   └── sftp/sshd_config        # 上传端只允许密钥认证和 SFTP
├── mosquitto/
│   ├── mosquitto.conf
│   └── certs/                 # 由证书脚本生成，不提交
├── postgres/init/
│   └── 01-create-service-databases.sql
└── scripts/
    ├── generate-mqtt-certs.sh
    ├── export-local-data.ps1
    ├── import-cloud-data.sh
    ├── diagnose-sub2api.sh
    ├── create-parent-account.sh
    ├── check-family-ai-account.sh
    ├── upgrade-cloud.sh
    ├── upgrade-service.sh
    ├── service-upgrade-worker.sh
    ├── generate-download-ssh-host-keys.sh
    ├── verify-download-service.sh
    └── check-stack.sh
```

## 2. 迁移步骤

### 2.1 在本地导出数据

先确保本地 Compose 正在运行，然后在平台仓库根目录执行：

```powershell
.\deploy\cloud\scripts\export-local-data.ps1
```

默认输出到 `deploy/cloud/migration-data/`，包含：

- `sprout_device_platform.dump`
- `sprout_sub2api.dump`
- `sprout_sub2api_data.tar.gz`（仅模型价格、页面和插件运行资源）
- `SHA256SUMS`

这个目录包含儿童相关数据，只能通过加密通道传输，导入完成后必须删除本地和
服务器上的临时副本。

Sub2API 的 `config.yaml` 和 `.installed` 不会导出。它们保存的是数据库密码、
Redis 密码和 JWT 密钥，只属于导出的那台服务器。云端会由 `.env` 和
`AUTO_SETUP` 重新生成，避免旧密码覆盖新环境。

### 2.2 上传部署包

在 1Panel 的“文件”中创建 `/opt/1panel/apps/sprout`，上传除
`migration-data` 外的整个 `deploy/cloud` 内容。数据包单独传到
`/opt/1panel/apps/sprout/migration-data`。

也可以使用 `scp`：

```bash
scp -r deploy/cloud/* root@<server>:/opt/1panel/apps/sprout/
scp -r deploy/cloud/migration-data root@<server>:/opt/1panel/apps/sprout/
```

### 2.3 生成生产密钥

```bash
cp .env.example .env
openssl rand -hex 32   # 分别生成数据库、Redis、令牌和加密密钥
```

每个密钥生成一次后立即写入 `.env`。不要使用同一个值填充多个字段，
`SPROUT_MFA_CREDENTIAL_KEY` 必须与 `SPROUT_AI_CREDENTIAL_KEY` 不同。

`SPROUT_SUB2API_API_KEY` 不能凭空生成。它必须与 `sprout_sub2api.api_keys`
中一条 active 记录一致。导出数据库后可用以下命令查询，并把输出填入
`.env`：

```bash
docker exec -i <postgres-container> psql -U sprout -d sprout_sub2api \
  -Atc "SELECT key FROM api_keys WHERE status = 'active' ORDER BY id LIMIT 1;"
```

不要直接复用本地 `voice_gateway` 容器环境变量中的值；当前本地环境已出现
数据库记录与容器变量不一致的情况。迁移时必须以数据库查询结果为准。

### 2.4 生成 MQTT 证书

在云端执行：

```bash
chmod +x scripts/*.sh
./scripts/generate-mqtt-certs.sh mqtt.<待确认域名>
```

参数必须是设备实际连接的主机名或公网 IP。不要把 `127.0.0.1` 用于生产，
否则设备会因证书域名不匹配而拒绝连接。

当前脚本生成一张共享设备证书，只用于单批次早期迁移。正式量产前必须改为
每台设备一张唯一证书，并为设备身份建立签发、吊销和轮换流程，这是项目 P0
安全约束要求。

### 2.5 在 1Panel 启动

在“容器 → 编排 → 创建编排”中选择
`/opt/1panel/apps/sprout/docker-compose.yml`，项目名使用 `sprout`。

第一次启动只启动 PostgreSQL、Redis 和 MQTT：

```bash
docker compose -f docker-compose.yml up -d --no-deps postgres redis mqtt
```

确认 PostgreSQL 已健康后导入迁移数据：

```bash
./scripts/import-cloud-data.sh migration-data
```

导入脚本会先校验 `SHA256SUMS`，再恢复两个数据库，并把 Sub2API 的非敏感
运行数据直接写入对应命名卷；缺少命名卷时脚本会先创建它。最后启动全部业务
服务：

```bash
docker compose -f docker-compose.yml up -d
./scripts/check-stack.sh
```

如果 Sub2API 反复重启，执行诊断脚本并把输出发回：

```bash
./scripts/diagnose-sub2api.sh
```

脚本只读取状态、日志和数据卷目录，不会停止容器或修改数据。

Sub2API 的 `TOTP_ENCRYPTION_KEY` 必须是 64 位十六进制字符串，不能保留
`.env.example` 中的 `replace-with...` 文本。启动前可以先执行：

```bash
./scripts/validate-cloud-env.sh
```

校验失败时脚本会明确指出缺少或格式错误的变量，不会启动容器。

### 2.6 配置发布文件服务

发布文件服务由两个容器组成：

- `download_ftp`：离线发布回退通道，使用密钥认证；默认只监听本机；
- `download_http`：只读 Nginx，公网只经 1Panel 反向代理访问。

首次部署先生成稳定的 SFTP 主机密钥和可选回退密钥：

```bash
./scripts/generate-download-ssh-host-keys.sh
./scripts/generate-download-release-key.sh
```

第二个脚本会把公钥放入 `download/sftp/authorized_keys/release.pub`，并输出
只在本机保存的私钥路径。GitHub Actions 默认通过 `api.clarkhub.cn` 的
受控内部发布接口上传，不需要公网开放 SFTP；如果管理端暂时不可达，才使用
`SPROUT_DOWNLOAD_SFTP_*` Secrets 执行离线回退上传。

`SPROUT_RELEASE_UPLOAD_TOKEN` 是发布流水线专用令牌。它只能调用
`/internal/v1/release-files` 和 `/internal/v1/release-index/refresh`，
不能读取家长账号、设备状态或 AI 凭据。将同一个值写入服务器 `.env` 和
GitHub Secret `SPROUT_RELEASE_UPLOAD_TOKEN`。发布工作流会先写入不可变
版本目录，最后原子替换 `index.json`，因此管理端和客户端不会读取到半成品版本。

在 1Panel 中为 `download.clarkhub.cn` 创建 HTTPS 反向代理，上游为
`http://127.0.0.1:8085`。Nginx 已关闭目录枚举，根路径返回 `404`，单个版本
清单和文件仍可下载。管理端只读取 `index.json` 和版本 `manifest.json`，
SFTP 凭据不会下发到客户端。

管理端“下载文件”页面直接读取同一个共享卷，因此能看到 CI 发布的文件和
管理员手动上传的文件，并显示每个文件是否已经登记到发布索引。手动上传时
目录由版本、平台和文件类型自动生成：Android APK 进入
`<version>/stable/android/apk/`，管理端压缩包进入
`<version>/stable/all/admin-web/`，其他资源包保留 `<kind>` 目录。上传成功后
如果版本、校验值和公开下载地址齐全，系统会同时登记一条草稿版本；是否对
客户端发布仍由“内容发布”页面手动确认。

发布文件查找优先使用已经登记的版本记录；如果 CI 只上传了文件、尚未登记，
则回退到真实下载清单，因此“自动查找更新文件”不会因为 CI 未写数据库而失败。
首次升级到包含本功能的版本时，`download_init` 会递归修正共享卷的组权限，
让 SFTP 与后端使用同一个可写组。

启动后执行：

```bash
./scripts/verify-download-service.sh
```

## 3. 反向代理

在 1Panel“网站”中创建以下反向代理：

| 域名 | 上游 | 用途 |
| --- | --- | --- |
| `admin.clarkhub.cn` | `http://127.0.0.1:8083` | 管理端 |
| `api.clarkhub.cn` | `http://127.0.0.1:8081` | 家长端和设备 API |
| `voice.clarkhub.cn` | `http://127.0.0.1:8082` | 语音网关，需开启 WebSocket |
| `sub.clarkhub.cn` | `http://127.0.0.1:8084` | Sub2API 管理界面和网关 API |
| `download.clarkhub.cn` | `http://127.0.0.1:8085` | 只读发布文件与版本索引 |

Sub2API 仅绑定宿主机回环地址，公网访问必须经过 1Panel 反向代理并使用
HTTPS。管理端和其他服务仍通过 Compose 内网域名调用 Sub2API，不经过公网。

Sub2API 的管理界面和 API 共用同一站点：访问域名根路径进入管理界面，
OpenAI 兼容接口继续使用 `/api/v1`。在 1Panel 中创建网站并设置反向代理后，
为 Sub2API 站点开启 WebSocket 和长连接支持，并关闭响应缓冲，避免流式输出
被整段缓存：

```nginx
proxy_buffering off;
proxy_cache off;
proxy_request_buffering off;
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;
send_timeout 3600s;
```

公网域名会同时暴露 Sub2API 的登录页和网关 API。必须使用强管理员密码、
启用 MFA、限制接口密钥权限和调用额度，并定期检查登录与调用审计。若后续
不再需要公网访问，删除该域名的反向代理即可，不影响 Compose 内部调用。

管理端镜像已内置 `/api/` 同源代理，不需要为管理端再单独配置 API 路径。

`api.clarkhub.cn` 还要承载发布流水线的文件上传，单个 APK 体积远大于 Nginx
默认的 1 MB 请求体上限。如果只配置普通反向代理，上传会在 TLS 握手后被
连接重置，`SPROUT_RELEASE_UPLOAD_TOKEN` 正确也无法成功。

1Panel 已经在 `server` 上下文写入了 `client_max_body_size`，同一上下文重复
声明会让 `nginx -t` 直接报 `directive is duplicate`。因此放宽上限必须放在
`location` 块内，用就近覆盖取代重复声明。站点配置里写的是 `/www/...`，
但 1Panel 的 `/www` 是指向 `/opt/1panel/www` 的软链接；如果 shell 中
`/www` 不可写或提示目录不存在，直接使用实际路径。在
`/opt/1panel/www/sites/api.clarkhub.cn/proxy/` 下新增
`release-upload.conf`：

```nginx
location ^~ /api/v1/release-publication/ {
    client_max_body_size 512m;
    client_body_timeout 600s;
    proxy_http_version 1.1;
    proxy_request_buffering off;
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
    send_timeout 600s;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_pass http://127.0.0.1:8081;
}
```

`proxy_pass` 的地址要与其他反向代理片段里的上游保持一致；如果面板已经为
该站点生成上游，可以把该值改成面板使用的 `upstream` 名称。该 `location`
只覆盖发布接口前缀，其余路径仍由面板生成的反向代理处理。保存后执行
`nginx -t && nginx -s reload`；此配置不改变鉴权规则。

设备 MQTT 使用独立域名（待确认），在 1Panel 或云防火墙中放行
`8883/tcp`。不要对公网放行 `1883/tcp`。

如果 MQTT 容器反复重启，先检查证书所有权和日志：

```bash
find mosquitto/certs -maxdepth 2 -type f -printf '%M %u:%g %p\n'
docker logs --tail=100 sprout-mqtt-1
```

证书按使用方分开存放：

- `mosquitto/certs/broker/server.key` 归 MQTT 用户所有，默认 UID/GID `1883`
- `mosquitto/certs/broker/healthcheck.key` 是本地健康检查证书，同样归 MQTT 用户
- `mosquitto/certs/device/device.key` 归平台服务用户所有，默认 UID/GID `65532`

两组私钥权限都是 `640`，证书是 `644`。不要把两组私钥放在同一个目录后
统一 `chown`，否则 broker 和平台服务之间必有一方无法读取。

如果是旧版本生成的证书目录，执行下面命令迁移到新结构并重启：

```bash
./scripts/generate-mqtt-certs.sh mqtt.<待确认域名>
docker compose --env-file .env up -d --force-recreate mqtt device_platform
```

脚本默认复用已有的 CA，只重新签发服务端和设备证书，因此不会改变设备
信任根。只有明确需要轮换整个信任链时才执行：

```bash
./scripts/generate-mqtt-certs.sh mqtt.<待确认域名> --rotate-ca
```

轮换后，已经安装旧 CA 的设备必须同步更新 `ca.crt`，否则设备会拒绝新证书。
证书目录中的私钥不应进入 Git。

## 4. 环境变量逐行说明

### PostgreSQL

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_POSTGRES_USER` | PostgreSQL 用户名 | 可保留 `sprout` |
| `SPROUT_POSTGRES_PASSWORD` | 数据库密码，被 device_platform 和 sub2api 共用 | 32 位以上随机值，不能与 Redis 相同 |
| `SPROUT_POSTGRES_DB` | 容器初始化时创建的默认库 | `sprout` |
| `SPROUT_DEVICE_PLATFORM_DATABASE_NAME` | 家长、儿童和设备业务库名 | `sprout_device_platform` |
| `SPROUT_SUB2API_DATABASE_NAME` | AI 账户、密钥和用量库名 | `sprout_sub2api` |

### Redis

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_REDIS_PASSWORD` | Redis 密码，被平台、语音网关和 sub2api 共用 | 32 位以上随机值 |

### 服务端口与监听地址

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_SERVICE_BIND_ADDRESS` | HTTP 服务的宿主监听地址 | 保持 `127.0.0.1`，由 1Panel 反代 |
| `SPROUT_MQTT_BIND_ADDRESS` | 明文 MQTT 的宿主监听地址 | 保持 `127.0.0.1`，禁止暴露公网 |
| `SPROUT_MQTTS_BIND_ADDRESS` | 加密 MQTT 的宿主监听地址 | `0.0.0.0`，公网只放行 8883 |
| `SPROUT_MQTT_UID` | 证书私钥归属的宿主 UID | 官方 Mosquitto 镜像默认 `1883` |
| `SPROUT_MQTT_GID` | 证书私钥归属的宿主 GID | 官方 Mosquitto 镜像默认 `1883` |
| `SPROUT_DEVICE_PLATFORM_UID` | 设备证书私钥归属的宿主 UID | 平台镜像默认 `65532` |
| `SPROUT_DEVICE_PLATFORM_GID` | 设备证书私钥归属的宿主 GID | 平台镜像默认 `65532` |
| `SPROUT_DEVICE_PLATFORM_PORT` | 设备平台宿主端口 | `8081` |
| `SPROUT_VOICE_GATEWAY_PORT` | 语音网关宿主端口 | `8082` |
| `SPROUT_ADMIN_WEB_PORT` | 管理端宿主端口 | `8083` |
| `SPROUT_SUB2API_PORT` | Sub2API 宿主端口，仅用于 1Panel 反向代理 | `8084` |
| `SPROUT_MQTT_PORT` | 明文 MQTT 端口 | 仅内网或其他服务使用 |
| `SPROUT_MQTTS_PORT` | 双向 TLS MQTT 端口 | 设备连接的端口，放行 8883 |
| `SPROUT_DOWNLOAD_SFTP_BIND_ADDRESS` | SFTP 上传端监听地址 | 只允许 CI 出口访问，禁止对公网开放 |
| `SPROUT_DOWNLOAD_SFTP_PORT` | SFTP 上传端端口 | 示例 `2022` |
| `SPROUT_DOWNLOAD_HTTP_BIND_ADDRESS` | 文件下载容器监听地址 | 保持 `127.0.0.1`，由 1Panel 反代 |
| `SPROUT_DOWNLOAD_HTTP_PORT` | 文件下载容器端口 | `8085` |
| `SPROUT_DOWNLOAD_SFTP_USER` | SFTP 上传用户 | 不要复用系统用户 |
| `SPROUT_DOWNLOAD_SFTP_UID` | SFTP 上传用户的 UID | 与卷所有者保持一致，示例 `1001` |
| `SPROUT_DOWNLOAD_SFTP_GID` | 下载目录的共享写组 | SFTP 与平台服务共同使用，示例 `1001` |
| `SPROUT_DOWNLOAD_SFTP_AUTHORIZED_KEYS_DIR` | CI 公钥目录 | 只放发布公钥，不提交私钥 |
| `SPROUT_DOWNLOAD_PUBLIC_BASE_URL` | 下载文件公开地址 | 必须使用 HTTPS，示例 `https://download.clarkhub.cn` |

### 镜像版本

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_PLATFORM_VERSION` | 平台三个镜像的版本 | 固定为 GitHub Release 版本，不用 `latest` |
| `SPROUT_SUB2API_VERSION` | AI 网关镜像版本 | 固定为 GitHub Release 版本 |

### 服务间共享密钥

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_INTERNAL_SERVICE_TOKEN` | device_platform 调用 sub2api 内部接口的令牌 | 32 位以上随机值，绝不能下发到客户端 |
| `SPROUT_RELEASE_UPLOAD_TOKEN` | 发布流水线上传 release 文件和刷新索引的令牌 | 只授予发布权限，32 位以上随机值 |
| `SPROUT_AUTH_ACCESS_TOKEN_SECRET` | 家长登录访问令牌的签名密钥 | 32 位以上随机值 |
| `SPROUT_AI_CREDENTIAL_KEY` | AI 账号凭据的加密密钥 | 32 位以上随机值 |
| `SPROUT_MFA_CREDENTIAL_KEY` | 管理端 MFA 凭据的加密密钥 | 与 AI 密钥不同，32 位以上 |
| `SPROUT_AUTH_ACCESS_TOKEN_TTL` | 访问令牌有效期 | 生产可保持 `15m` |
| `SPROUT_AUTH_REFRESH_TOKEN_TTL` | 刷新令牌有效期 | `720h` 表示 30 天 |
| `SPROUT_MFA_CHALLENGE_TTL` | MFA 挑战有效期 | 生产可保持 `5m` |

### 家长手机号验证

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_PHONE_VERIFICATION_MODE` | 手机号验证模式 | 生产必须是 `disabled`，接入真实短信后再扩展 |
| `SPROUT_ALLOW_LOCAL_SMS_BYPASS` | 是否允许本地验证码直通 | 生产必须为 `false` |

`local` 模式会接受空验证码或 `000000`，只允许在开发环境使用。

### AI 账号默认值

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_AI_DEFAULT_BALANCE_USD` | 新 AI 账号初始余额 | 按运营策略设置 |
| `SPROUT_AI_DEFAULT_CONCURRENCY` | 新 AI 账号默认并发 | 建议从 `1` 开始 |
| `SPROUT_AI_DEFAULT_MODELS` | 新账号默认允许的模型，逗号分隔 | 留空表示不额外限制 |

### Sub2API 出站域名白名单

`SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS` 是 Sub2API 允许连接的上游域名列表。
该变量是完整覆盖而非追加，多个域名使用英文逗号分隔，不能包含协议、端口或
路径。新增中转站时必须保留当前正在使用的全部域名，否则其他账号会立即失效。

```env
SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS=sub.unsee.you,api.openai.com,api.anthropic.com,api.kimi.com,api.moonshot.ai,api.moonshot.cn,open.bigmodel.cn,api.minimaxi.com,api.minimax.io,opencode.ai,generativelanguage.googleapis.com,cloudcode-pa.googleapis.com,*.openai.azure.com
```

修改后必须重建 Sub2API 容器，环境变量不会在运行中的容器内自动刷新。

### Sub2API

| 变量 | 作用 | 生产建议 |
| --- | --- | --- |
| `SPROUT_SUB2API_API_KEY` | voice_gateway 调用 AI 网关的密钥 | 必须与数据库中的 active key 一致 |
| `SPROUT_SUB2API_ADMIN_EMAIL` | Sub2API 初始化管理员邮箱 | 使用真实可用邮箱 |
| `SPROUT_SUB2API_ADMIN_PASSWORD` | Sub2API 管理员密码 | 强随机密码 |
| `SPROUT_SUB2API_JWT_SECRET` | Sub2API 会话签名密钥 | 32 位以上随机值 |
| `SPROUT_SUB2API_TOTP_ENCRYPTION_KEY` | Sub2API 的 TOTP 加密密钥 | 必须为 64 位十六进制字符串，且必须固定 |
| `SPROUT_TIMEZONE` | 容器时区 | `Asia/Shanghai` |

## 5. 升级方式

升级前先确认 GitHub Release 已发布。平台版本来自
`TissyBoxC/sprout-platform`，Sub2API 版本来自
`TissyBoxC/sprout-sub2api-fork`。不要把 `latest` 写入 `.env`。

在服务器上执行：

```bash
cd /opt/1panel/apps/sprout/deploy/cloud
chmod 600 .env

# 推荐：脚本会先备份两个数据库，再切换版本、拉取镜像、重建并检查健康状态。
 ./scripts/upgrade-cloud.sh 0.10.0 0.2.16
```

省略第二个参数时保留当前 Sub2API 版本：

```bash
 ./scripts/upgrade-cloud.sh 0.10.0
```

脚本会执行以下检查与操作：

1. 校验 `.env`、版本格式，以及当前目录是否为干净 Git 工作区。
2. 启动并在必要时等待 PostgreSQL 就绪。
3. 用 `pg_dump` 备份 `sprout_device_platform` 和 `sprout_sub2api`，默认写入
   `deploy/cloud/migration-data/backups/<时间戳>/`，同时生成
   `SHA256SUMS`。
4. 在同一个目录中写入版本元数据，并用同目录临时文件原子替换 `.env` 中的
   `SPROUT_PLATFORM_VERSION` 和 `SPROUT_SUB2API_VERSION`。
5. 只拉取平台和 AI 网关版本化镜像，然后执行
   `docker compose up -d --remove-orphans`。
6. 运行 `scripts/check-stack.sh`，逐个检查设备平台、语音网关、Sub2API 和
   管理端。

如备份需要放到独立磁盘或加密目录，可在执行前覆盖默认位置：

```bash
SPROUT_CLOUD_BACKUP_DIR=/srv/sprout-backups \
  ./scripts/upgrade-cloud.sh 0.10.0 0.2.16
```

备份目录包含儿童和业务数据，必须限制权限、通过加密通道同步到异地，并在
保留期结束后删除。脚本不会自动恢复数据库，因为迁移后盲目回滚可能覆盖新
写入的有效数据。升级失败时脚本会打印旧版本号、备份路径和容器回滚命令；
只有确认数据库损坏且校验通过后，才手工执行数据库恢复。

### 5.1 失败回滚

先用 `sha256sum -c SHA256SUMS` 校验备份目录，然后按脚本输出的步骤恢复旧
版本：

```bash
cd /opt/1panel/apps/sprout/deploy/cloud
cd migration-data/backups/<时间戳>
sha256sum -c SHA256SUMS
cd /opt/1panel/apps/sprout/deploy/cloud
docker compose -f docker-compose.yml --env-file .env stop device_platform sub2api
```

确认没有用户正在写入后，再按下面模板恢复数据库。把 `<时间戳>` 替换为实际
备份目录名。`pg_restore --clean --if-exists` 会先删除现有对象，属于高风险
操作：

```bash
postgres_id="$(docker compose -f docker-compose.yml --env-file .env ps -q postgres)"
postgres_user="$(sed -n 's/^SPROUT_POSTGRES_USER=//p' .env | head -n 1)"
device_platform_database="$(sed -n 's/^SPROUT_DEVICE_PLATFORM_DATABASE_NAME=//p' .env | head -n 1)"
sub2api_database="$(sed -n 's/^SPROUT_SUB2API_DATABASE_NAME=//p' .env | head -n 1)"
docker exec -i "$postgres_id" pg_restore \
  -U "$postgres_user" -d "$device_platform_database" \
  --clean --if-exists --no-owner \
  < "migration-data/backups/<时间戳>/sprout_device_platform.dump"
docker exec -i "$postgres_id" pg_restore \
  -U "$postgres_user" -d "$sub2api_database" \
  --clean --if-exists --no-owner \
  < "migration-data/backups/<时间戳>/sprout_sub2api.dump"
docker compose -f docker-compose.yml --env-file .env up -d --remove-orphans
./scripts/check-stack.sh
```

如果数据库名或用户名在 `.env` 中改过，命令中的值也必须同步修改。数据库
恢复属于高风险操作，执行前应停止 `device_platform` 和 `sub2api`，并确认
没有用户正在写入。

无论升级成功还是失败，都不要执行：

```bash
docker compose down -v
```

`-v` 会删除 PostgreSQL、Redis、MQTT 和 Sub2API 的命名卷。正常升级只需要
`up -d --remove-orphans`。

### 5.2 资源更新与客户端更新

`.env` 中预留了三个不含密钥的基础地址：

| 变量 | 作用 |
| --- | --- |
| `SPROUT_OTA_MANIFEST_BASE_URL` | 版本清单入口，用于判断是否有小升级或大升级 |
| `SPROUT_OTA_RESOURCE_BASE_URL` | 资源包入口，用于文案、主题和内容资源的小升级 |
| `SPROUT_OTA_CLIENT_BASE_URL` | APK 或客户端安装包入口，用于大升级下载 |
| `SPROUT_OTA_FIRMWARE_SIGNATURE_PUBLIC_KEYS` | 固件签名公钥环，JSON 格式，键为密钥编号、值为 Base64 Ed25519 公钥；发布固件前必须配置 |

这些地址必须是 HTTPS，默认都指向 `https://download.clarkhub.cn`。发布流水线
会在每次版本 Release 后上传 APK、管理端包、契约包、服务二进制和
`SHA256SUMS`，并生成带有版本标签的 `index.json` 与 `manifest.json`。管理端
通过索引自动取得链接和校验值，不需要运营人员手工填写。

下载服务关闭目录枚举，SFTP 私钥只存在 GitHub Actions Secret 和服务器公钥
目录中，不能写入 `.env` 下发给设备或家长端。

在 GitHub 仓库中配置以下 Secret：

| Secret | 用途 |
| --- | --- |
| `SPROUT_RELEASE_UPLOAD_TOKEN` | HTTPS 管理端发布令牌，必须与服务器 `.env` 一致 |

`SPROUT_DOWNLOAD_PUBLIC_URL` 不是 Secret，集中维护在
`deploy/public-endpoints.env`。

### 5.3 品牌级服务版本管理与自动升级

管理端的“系统设置 → 服务版本”是品牌级运维入口，不是“初芽”单个产品的功能页面。
它管理整个“如此萌屋”品牌在服务器上的运行组件，因此升级对象使用服务标识，而不是
产品名称或设备型号。当前纳入自动升级的服务只有：

| 服务标识 | 含义 | 升级方式 |
| --- | --- | --- |
| `sub2api` | 品牌 AI 网关 | 独立镜像，独立版本 |
| `device_platform` | 设备平台 | 平台版本镜像 |
| `voice_gateway` | 语音网关 | 平台版本镜像 |
| `admin_web` | 品牌管理端 | 平台版本镜像 |

`postgres`、`redis`、`mqtt` 以及 `download_init`、`download_ftp`、`download_http`
只做状态检测，不会被自动升级。基础设施升级涉及数据兼容、停机窗口和证书，必须
单独评审并执行完整备份流程。

管理端把共享状态写入 `SPROUT_SERVICE_VERSION_STATE_DIR`，默认是命名卷
`sprout_upgrade_data` 内的 `/var/lib/sprout-upgrades`。该目录同时挂载给
`device_platform` 和独立容器 `upgrade_worker`。生产部署目录不是默认
`/opt/1panel/apps/sprout/deploy/cloud` 时，必须同步修改
`SPROUT_SERVICE_VERSION_HOST_CLOUD_DIR`，否则 Docker CLI 会看不到 Compose
引用的证书、下载目录和 `.env`：

```text
/var/lib/sprout-upgrades/
├── status.json                        # 定时刷新的服务版本快照
├── check-request.json                 # 管理端请求立即检查后由 worker 删除
├── queue/<operation_id>.json          # 管理端原子写入的升级请求
├── processing/<operation_id>.json     # worker 取走后的执行中请求
└── results/<operation_id>.json        # 可轮询的执行进度与结果
```

`status.json` 的 `current_version` 从正在运行的容器镜像标签读取，不是从 `.env`
推断。`latest_version` 只来自 GitHub Release；网络失败、仓库不可达或版本格式不正确
时该字段留空并且状态为 `unknown`，不会编造版本。

默认每 300 秒缓存一次 GitHub Release 查询，避免未认证请求触发限流。管理员在
页面点击“检查更新”时，worker 会立即绕过缓存刷新一次快照；不影响定时状态展示。

升级请求字段固定为：

```json
{
  "id": "operation-id",
  "target_service": "device_platform",
  "target_version": "0.10.0",
  "requested_at": "2026-10-03T10:00:00Z",
  "requested_by": "admin-account-id"
}
```

worker 只接受二进制/脚本内置的服务与版本白名单，不使用 `eval`，不会把请求字段
拼进 shell 命令，也不会把 `.env`、令牌、密钥或密码写入结果。升级顺序始终是
`sub2api → device_platform → voice_gateway → admin_web`，同一时间只执行一个任务。
执行器先拉取目标镜像，再原子替换 `.env` 中的版本号，然后使用
`docker compose up -d --no-deps <service>` 只重建目标服务。失败时会尝试恢复旧镜像
和旧版本号，并把结果标记为 `failed`。

`admin_web` 自升级是允许的：`upgrade_worker` 与 `admin_web` 是两个不同容器，
升级管理端不会中断 worker。管理端重启期间结果文件仍会持续写入，完成后包含
`succeeded` 和“管理端已重启，稍后自动恢复”状态，浏览器刷新后即可继续查看。

#### 权限与安全边界

`upgrade_worker` 挂载 `/var/run/docker.sock`，这等价于宿主机 `root` 权限。请同时满足：

- worker 不发布任何端口，不接受外网连接，只从共享命名卷读取请求。
- 只有管理端后端可以写入 `check-request.json` 和 `queue/*.json`；浏览器用户不能直接
  写服务器文件。
- 队列文件名、`operation_id`、服务名和目标版本都经过格式校验。
- 结果目录和状态目录由 worker 以受限权限写入，管理端只能读取。
- 不要把 `SPROUT_SERVICE_VERSION_GITHUB_TOKEN` 输出到日志、结果或界面；该变量只用于
  提高 GitHub API 限流额度，不是必需项。

#### 排障

先确认 worker 与共享目录：

```bash
cd /opt/1panel/apps/sprout/deploy/cloud
docker compose --env-file .env ps upgrade_worker device_platform admin_web
docker compose --env-file .env logs --tail=200 upgrade_worker
```

再查看状态和最近一次结果：

```bash
docker compose --env-file .env exec -T upgrade_worker \
  sh -c 'ls -la /var/lib/sprout-upgrades; cat /var/lib/sprout-upgrades/status.json'
docker compose --env-file .env exec -T upgrade_worker \
  sh -c 'ls -la /var/lib/sprout-upgrades/results'
```

如果任务长期停留在 `running`，检查目标服务健康状态和 worker 日志：

```bash
docker compose --env-file .env ps
docker compose --env-file .env logs --tail=200 device_platform
docker compose --env-file .env logs --tail=200 voice_gateway
docker compose --env-file .env logs --tail=200 admin_web
docker compose --env-file .env logs --tail=200 sub2api
```

如果 `status.json` 一直是 `unknown`，先确认 worker 容器能访问 GitHub，以及
`SPROUT_UPGRADE_PLATFORM_REPOSITORY`、`SPROUT_UPGRADE_SUB2API_REPOSITORY` 没有被改错。

#### 手动校验

在服务器上可以直接调用执行器，检查结果不会修改 `.env`：

```bash
./scripts/upgrade-service.sh --check all
./scripts/upgrade-service.sh --check admin_web
```

执行单服务升级时，命令会再次校验 Git 工作区、`.env` 和 Compose 配置：

```bash
./scripts/upgrade-service.sh admin_web
```

### 5.4 云端一键升级

代码推送到 `main` 并等待 GitHub Actions 完成中文 Release 后，在服务器执行：

```bash
cd /opt/1panel/apps/sprout
git pull --ff-only
cd deploy/cloud
chmod 600 .env
./scripts/upgrade-cloud.sh 0.10.0 0.2.16
```

脚本会先备份两个数据库，再原子切换镜像版本、拉取发布镜像、重建服务并检查
健康状态。平台 `VERSION` 是正式版本唯一来源；发布和 Docker 镜像必须使用相同
版本号，不允许只更新其中一侧。只升级平台服务时可省略 Sub2API 版本参数：

```bash
./scripts/upgrade-cloud.sh 0.10.0
```

脚本不会自动删除数据卷，也不会在失败时自动恢复数据库。失败输出会保留旧版本
号、备份目录和回滚命令；只有确认数据库损坏并验证备份校验值后，才执行上一节
的手工恢复。

## 6. 创建家长账号

云端已经包含 `/device-platform-admin` 维护命令。创建账号时不会把密码写入
shell 历史，脚本也不会在输出中打印密码。

```bash
cd /opt/1panel/apps/sprout/deploy/cloud

SPROUT_PARENT_PASSWORD='替换为至少8位且包含字母和数字的密码' \
  ./scripts/create-parent-account.sh \
    --phone 13800138000 \
    --guardian-family-name 王 \
    --child-nickname 小芽 \
    --child-birthday 2022-05-20
```

如果不设置 `SPROUT_PARENT_PASSWORD`，脚本会在终端中隐藏输入密码。也可以用
`--password`，但不建议，因为参数可能进入 shell 历史。

创建后检查家长账号和附属 AI 账号：

```bash
./scripts/check-family-ai-account.sh --phone 13800138000
```

输出只包含脱敏手机号、家长状态、AI 账号状态、凭据是否就绪和余额，不输出
邮箱、API Key 或数据库密文。如果 AI 账号显示“未创建”或“未就绪”，先确认
Sub2API 健康并查看诊断输出：

```bash
./scripts/diagnose-sub2api.sh
```

管理端“家长账号”页面也提供“重新开通 AI 服务”。该操作不会创建第二个家长
账号，只会复用现有家长身份幂等修复附属 AI 账号和凭据。

## 7. 安全清单

- `.env` 权限设为 `600`，不提交 Git。
- Sub2API 只绑定 `127.0.0.1`，公网入口由 1Panel 终止 HTTPS 后转发；禁止
  直接映射到 `0.0.0.0`。数据库端口始终不暴露公网。
- 生产环境禁用手机号验证直通。
- 生产环境替换 MQTT 证书，并启用域名匹配校验。
- 迁移完成后删除服务器上的 `migration-data` 和本地导出包。
- 在 1Panel 中启用 HTTPS，并确认管理端、API 和语音域名均使用证书。
