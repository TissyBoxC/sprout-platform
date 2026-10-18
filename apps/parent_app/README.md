<div align="center">

<img src="../../assets/brand/sprout/brand_banner.png" alt="如此萌屋" width="560" />

</div>

# 如此萌屋家长端

面向家长和监护人的 Flutter 应用，是芽系列·初芽平台的主要家庭入口。

公网 API 地址统一维护在 `deploy/public-endpoints.env`，当前为
`https://api.clarkhub.cn`。Release 构建必须传入该地址。

## 职责

- 登录、家庭创建和监护人身份验证。
- 儿童档案、年龄分级和家长策略管理。
- 游戏机绑定、在线状态和基础设备管理。
- 远程留言、内容管理和 OTA 更新入口。
- 使用报告、隐私授权和儿童数据删除入口。

应用只访问平台业务服务，不直接连接设备，也不保存模型供应商密钥。

## 技术栈

| 场景 | 选型 |
| --- | --- |
| 框架 | Flutter、Dart |
| 路由 | `go_router` |
| 状态管理 | `flutter_riverpod` |
| 网络 | `dio` |
| 安全存储 | `flutter_secure_storage` |
| 序列化 | `json_serializable`、`json_annotation` |
| 测试 | `flutter_test`、`flutter_lints` |

## 本地运行

```powershell
flutter pub get
flutter run -d android --dart-define=API_BASE_URL=http://10.0.2.2:8081
```

Android 模拟器访问本机服务使用 `10.0.2.2`；真机请改为电脑在局域网中的地址。
Android 模拟器未传入 `API_BASE_URL` 时默认连接 `http://10.0.2.2:8081`。
其他平台或真机必须传入实际服务地址。

构建可安装的 Android 包时，必须显式传入 HTTPS 服务地址：

```powershell
flutter build apk --release --dart-define=API_BASE_URL=https://api.clarkhub.cn
```

Release 构建会拒绝回退到模拟器地址或非 HTTPS 地址。需要覆盖请求超时时，可同时传入
`--dart-define=API_TIMEOUT_SECONDS=15`。

当前维护范围只有 Android 和 iOS。Linux、Windows、macOS 和 Web
不属于家长端交付目标，因此没有保留对应平台目录。

## 目录

```text
lib/app/         应用入口、路由和主题装配
lib/core/        配置、网络、存储、错误和共享 UI 文案
lib/features/    按业务能力拆分的功能模块
lib/shared/      跨功能复用的无业务组件
test/            单元测试和组件测试
assets/brand/    品牌展示资源；应用图标由 flutter_launcher_icons 生成
```

每个 feature 必须通过公开边界访问，删除无关功能时不得破坏其他 feature
的编译、测试和运行。

## 质量门禁

```powershell
flutter analyze
flutter test
```
