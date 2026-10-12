package service

import "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"

// Definition is one immutable feature registration.
type Definition struct {
	Feature       domain.Feature
	ImmutableKeys map[string]struct{}
}

// DefaultRegistry returns the complete feature center registration catalogue.
func DefaultRegistry() []Definition {
	return []Definition{
		featureDef("auth", "账号登录", "家长和管理员的登录、会话与安全验证。", "identity", "账号平台", true,
			configFields(
				boolField("registration_enabled", "允许注册", true, true),
				boolField("phone_verification_required", "需要手机验证", true, true),
				boolField("email_login_enabled", "允许邮箱登录", true, true),
			),
			metrics("login_success_rate", "登录成功率", "percent")),
		readOnlyFeatureDef("parent_account", "家长账号", "家长资料、密码与账号状态管理。", "identity", "账号平台",
			"家长资料、密码和状态在家长账号页面逐项管理，功能中心只提供能力状态和统计。",
			metrics("active_parent_count", "有效家长数", "个")),
		featureDef("ai_account", "AI 账号", "家长 AI 账号的额度、并发与模型配置。", "ai", "AI 服务", true,
			configFields(
				numberField("default_balance_usd", "默认额度", float64(0), true, true, ""),
				integerField("default_concurrency", "默认并发", float64(1), true, true, "路"),
				customMultiselectField("default_models", "默认模型", []string{}),
			),
			metrics("active_ai_account_count", "有效 AI 账号", "个")),
		readOnlyFeatureDef("child_profile", "儿童档案", "儿童昵称、生日和年龄分级资料。", "family", "家庭平台",
			"儿童资料按家庭逐个管理，功能中心只提供能力状态和统计。",
			metrics("child_profile_count", "儿童档案数", "个")),
		readOnlyFeatureDef("parent_policy", "家长策略", "内容时长、分类和禁用时段策略。", "family", "家庭平台",
			"家长策略按儿童分别设置，功能中心只提供能力状态和统计。",
			metrics("policy_update_count", "策略更新数", "次")),
		readOnlyFeatureDef("device_binding", "设备绑定", "设备注册、认证和家庭绑定关系。", "device", "设备平台",
			"设备绑定在设备管理页面操作，功能中心只提供能力状态和统计。",
			metrics("bound_device_count", "已绑定设备", "台")),
		readOnlyFeatureDef("device_runtime", "设备运行", "在线状态、心跳和远程指令。", "device", "设备平台",
			"设备在线状态和远程指令由设备运行页面管理，功能中心只提供能力状态和统计。",
			metrics("online_device_count", "在线设备", "台")),
		readOnlyFeatureDef("device_diagnostics", "设备诊断", "设备运行日志、配网和故障快照。", "device", "设备平台",
			"诊断记录由设备诊断页面查看和导出，功能中心只提供能力状态和统计。",
			metrics("diagnostic_event_count", "诊断事件", "条")),
		readOnlyFeatureDef("content_library", "内容库", "儿童内容包的审核、发布与下发。", "content", "内容平台",
			"内容包在内容库页面审核和发布，功能中心只提供能力状态和统计。",
			metrics("published_content_count", "已发布内容", "个")),
		readOnlyFeatureDef(
			"ota_release",
			"设备更新",
			"设备固件版本的签名、灰度与回滚发布。",
			"release",
			"发布平台",
			"固件版本在设备 OTA 页面发布和管理，功能中心只提供能力状态和统计。",
			metrics("published_firmware_count", "已发布版本", "个"),
		),
		featureDef("app_update", "应用更新", "家长端应用的版本提示和强制更新策略。", "release", "发布平台", true,
			configFields(
				selectField("default_channel", "默认渠道", "stable", []domain.ConfigOption{
					{Value: "stable", Label: "稳定版"},
					{Value: "beta", Label: "测试版"},
					{Value: "canary", Label: "灰度版"},
				}, true),
				stringField("minimum_client_version", "最低客户端版本", "", true),
				stringField("mandatory_update_threshold", "强制更新线", "", true),
			),
			metrics("app_release_count", "已发布应用版本", "个")),
		featureDef("download_server", "下载服务", "内容、更新和应用文件的分发。", "release", "发布平台", false,
			nil,
			metrics("download_file_count", "发布文件", "个")),
		readOnlyFeatureDef("service_version", "服务版本", "平台服务版本的检查和升级。", "platform", "平台运维",
			"服务版本升级在服务版本页面执行，功能中心只提供能力状态和统计。",
			metrics("outdated_service_count", "待升级服务", "个")),
		readOnlyFeatureDef("ui_text", "界面文案", "客户端和管理端的文案配置。", "content", "内容平台",
			"文案在界面文案页面审核和发布，功能中心只提供能力状态和统计。",
			metrics("published_ui_text_count", "已发布文案", "个")),
		readOnlyFeatureDef("voice_gateway", "语音网关", "儿童实时语音会话和语音安全。", "voice", "语音平台",
			"语音网关的连接、密钥和会话策略由部署配置管理，功能中心只提供健康状态和统计。",
			metrics("active_voice_session_count", "实时会话", "路")),
		featureDef("voice_conversation", "语音对话", "全双工、连续会话与远场拾音的默认策略。", "voice", "语音平台", true,
			configFields(
				boolField("continuous_conversation_enabled", "默认启用连续会话", true, true),
				boundedIntegerField("idle_window_seconds", "空闲保持时长", 8, 2, 60, true, "秒"),
				boolField("barge_in_enabled", "允许打断语音", true, true),
				boolField("far_field_enabled", "启用远场拾音", true, true),
			),
			metrics(
				"voice_session_turn_count", "对话轮次", "次",
				"voice_session_barge_in_count", "打断次数", "次",
				"voice_session_continuous_rate", "连续会话占比", "percent",
			)),
		readOnlyFeatureDef("ai_model_gateway", "模型网关", "模型路由、限额与供应商健康。", "ai", "AI 服务",
			"模型供应商和路由由 AI 服务统一管理，功能中心只提供健康状态和统计。",
			metrics("model_request_count", "模型调用", "次")),
		featureDef("usage_report", "使用报告", "设备使用时长和内容统计。", "family", "家庭平台", true,
			configFields(
				boundedIntegerField("retention_days", "数据保留天数", 365, 1, 3650, true, "天"),
			),
			metrics("reported_usage_days", "已上报天数", "天")),
		readOnlyFeatureDef("telemetry", "遥测监控", "设备心跳、网络质量、温度与积压趋势。", "platform", "平台运维",
			"遥测数据由设备心跳和使用报告聚合产生，功能中心只提供实时状态和统计。",
			metrics(
				"telemetry_device_count", "接入设备", "台",
				"telemetry_online_count", "在线设备", "台",
				"telemetry_pending_total", "待上报积压", "条",
			)),
		readOnlyFeatureDef("notification", "消息通知", "家长站内通知、远程留言、短信与消息触达。", "platform", "平台运维",
			"站内通知始终可用；推送、短信和邮件由部署侧发送器配置管理，功能中心只展示真实配置状态和投递指标。",
			metrics(
				"notification_total", "通知总数", "条",
				"notification_pending_delivery", "待投递", "条",
				"notification_failed_delivery", "投递失败", "条",
				"notification_configured_channel_count", "已配置外部通道", "个",
			)),
		featureDef("platform_security", "平台安全", "管理员权限、审计与安全策略。", "security", "安全团队", true,
			configFields(
				boolField("minor_mode_default", "默认启用未成年人模式", true, true),
				boolField("output_moderation_enabled", "启用输出审核", true, true),
				boolField("crisis_intervention_enabled", "启用危机干预", true, true),
			),
			metrics("blocked_login_count", "已阻止登录", "次")),
		readOnlyFeatureDef("vision", "摄像头与视觉", "拍照识物、绘本识别与拍照问答。", "device", "视觉平台",
			"视觉任务由设备上报、语音网关执行，家长端控制授权；功能中心只提供能力状态和统计。",
			metrics(
				"vision_task_count", "视觉任务", "次",
				"vision_failed_task_count", "失败任务", "次",
			)),
		readOnlyFeatureDef("display", "屏幕与护眼", "亮度、色温、护眼策略和屏幕状态。", "device", "屏幕平台",
			"亮度、色温和护眼策略由家长端逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"display_device_count", "接入设备", "台",
				"display_touch_device_count", "触摸屏设备", "台",
			)),
		readOnlyFeatureDef("touch", "触摸交互", "点击、滑动、手势与触觉反馈。", "device", "触摸平台",
			"手势与触觉反馈由家长端逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"touch_device_count", "触摸设备", "台",
				"touch_haptic_device_count", "触觉设备", "台",
			)),
		readOnlyFeatureDef("screen", "屏幕内容", "表情、动画、点读与视频播放。", "content", "屏幕平台",
			"点读字典和视频播放由设备资源与家长端设置管理，功能中心只提供能力状态和统计。",
			metrics(
				"screen_device_count", "接入设备", "台",
				"screen_point_read_device_count", "支持点读", "台",
			)),
		readOnlyFeatureDef("connectivity", "蜂窝网络", "eSIM、流量、漫游与联网切换。", "device", "网络平台",
			"套餐、eSIM、流量和漫游策略由家长端逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"cellular_device_count", "蜂窝设备", "台",
				"cellular_roaming_count", "漫游设备", "台",
			)),
		readOnlyFeatureDef("power", "电池与低功耗", "充电、电量估算、低功耗与休眠。", "device", "电源平台",
			"低功耗与休眠策略由家长端逐设备设置并由设备执行，功能中心只提供能力状态和统计。",
			metrics(
				"power_device_count", "接入设备", "台",
				"power_low_power_count", "低功耗", "台",
				"power_deep_sleep_count", "深度休眠", "台",
			)),
		readOnlyFeatureDef("motion", "动作与电机", "电机、舵机与动作编排。", "device", "动作平台",
			"动作编排由家长端触发、设备执行并可随时停止，功能中心只提供能力状态和统计。",
			metrics(
				"motion_device_count", "接入设备", "台",
				"motion_running_count", "执行中", "台",
				"motion_faulted_count", "故障", "台",
			)),
		readOnlyFeatureDef("form_factor", "形态能力", "按能力集合切换的形态配置。", "device", "设备平台",
			"设备形态由出厂配置和 capability 决定，功能中心只提供能力状态和统计。",
			metrics(
				"form_factor_device_count", "接入设备", "台",
				"form_factor_action_count", "支持动作", "台",
			)),
		readOnlyFeatureDef("video_companion", "视频陪伴", "联系人与视频通话闭环。", "voice", "视频平台",
			"视频联系人和通话由家长端发起、设备确认，功能中心只提供能力状态和统计。",
			metrics(
				"video_call_count", "通话总数", "次",
			)),
		readOnlyFeatureDef("video_session", "视频会话", "摄像头与麦克风媒体会话协商。", "voice", "视频平台",
			"媒体会话授权由监护人逐项开启，功能中心只提供能力状态和统计。",
			metrics(
				"video_session_device_count", "接入设备", "台",
			)),
		readOnlyFeatureDef("wearable", "穿戴与定位", "定位、联系人、电子围栏与 SOS。", "device", "穿戴平台",
			"定位精度、围栏和 SOS 联系人由家长端逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"wearable_device_count", "穿戴设备", "台",
				"wearable_sos_event_count", "SOS 事件", "次",
			)),
		readOnlyFeatureDef("device_mesh", "多设备联动", "设备组、家庭联动与内容同步。", "device", "设备平台",
			"设备分组和联动场景由家长端配置，功能中心只提供能力状态和统计。",
			metrics(
				"device_group_count", "设备组", "个",
				"device_mesh_pending_sync_count", "待同步设备", "台",
			)),
		readOnlyFeatureDef("bluetooth_audio", "蓝牙音箱", "蓝牙音箱模式、配对与音频输入。", "device", "音频平台",
			"蓝牙模式由家长端逐设备启用，功能中心只提供能力状态和统计。",
			metrics(
				"bluetooth_device_count", "接入设备", "台",
				"bluetooth_connected_count", "已连接", "台",
			)),
		readOnlyFeatureDef("learning_visualization", "学习可视化", "可视化学习内容与反馈。", "content", "学习平台",
			"学习可视化策略由家长端逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"learning_visualization_device_count", "接入设备", "台",
			)),
		readOnlyFeatureDef("image_privacy", "图片隐私", "图片本地处理与上传授权。", "security", "安全团队",
			"图片处理模式与上传授权由监护人逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"image_privacy_device_count", "接入设备", "台",
				"image_privacy_upload_allowed_count", "允许上传", "台",
			)),
		readOnlyFeatureDef("english_learning", "英语学习", "课程、跟读练习与发音反馈。", "content", "学习平台",
			"课程内容与练习进度由家长端设备查看，功能中心只提供能力状态和统计。",
			metrics(
				"english_course_count", "课程数", "门",
				"english_practice_count", "练习记录", "次",
			)),
		readOnlyFeatureDef("alarm_reminder", "闹钟与定时提醒", "家庭闹钟、定时提醒与设备响铃记录。", "family", "家庭平台",
			"闹钟由家长端按家庭管理，设备按已绑定会话同步并回报响铃状态；功能中心只提供能力状态和统计。",
			metrics(
				"alarm_reminder_count", "已设置提醒", "条",
				"alarm_reminder_enabled_count", "启用中", "条",
				"alarm_reminder_event_count", "近 7 天响铃记录", "次",
			)),
		readOnlyFeatureDef("companionship", "陪伴与成长", "成长记录、鼓励与习惯提醒。", "family", "家庭平台",
			"成长记录和习惯提醒由家长端查看与设置，功能中心只提供能力状态和统计。",
			metrics(
				"companionship_record_count", "成长记录", "条",
				"habit_reminder_count", "习惯提醒", "条",
			)),
		readOnlyFeatureDef("local_command", "本地指令", "断网可用的本地设备指令。", "content", "内容平台",
			"本地指令随离线内容下发并由设备执行，功能中心只提供能力状态和统计。",
			metrics(
				"local_command_count", "本地指令", "条",
			)),
		readOnlyFeatureDef("offline_content", "离线内容", "内容离线清单、缓存与下载策略。", "content", "内容平台",
			"离线下载策略由家长端逐设备设置，功能中心只提供能力状态和统计。",
			metrics(
				"offline_policy_device_count", "启用设备", "台",
			)),
		readOnlyFeatureDef("content_engagement", "内容收藏与历史", "家长端内容收藏与播放记录。", "content", "内容平台",
			"收藏与播放历史由家长端按家庭维护，功能中心只提供能力状态和统计。",
			metrics(
				"content_favorite_count", "收藏数", "条",
				"content_play_history_count", "播放记录", "条",
			)),
		readOnlyFeatureDef("content_theme_pack", "主题内容包", "按主题编排的内容包与批量下载。", "content", "内容平台",
			"主题包在内容库页面编辑与发布，功能中心只提供能力状态和统计。",
			metrics(
				"content_theme_pack_count", "已发布主题包", "个",
			)),
		readOnlyFeatureDef("playback", "家长点播", "家长发起的按需播放请求闭环。", "content", "内容平台",
			"点播请求由家长端发起、设备确认并回传结果，功能中心只提供能力状态和统计。",
			metrics(
				"playback_request_count", "点播请求", "次",
				"playback_pending_count", "待处理", "次",
				"playback_failed_count", "失败", "次",
			)),
		readOnlyFeatureDef("recommendation", "内容推荐", "按年龄层与播放信号的内容推荐。", "content", "内容平台",
			"推荐由平台基于已发布内容与播放信号生成，功能中心只提供能力状态和统计。",
			metrics(
				"recommendation_candidate_count", "可推荐内容", "个",
			)),
		readOnlyFeatureDef("privacy_lifecycle", "隐私与数据生命周期", "监护人授权、导出与账号注销清理。", "security", "安全团队",
			"授权、导出和注销清理由监护人自助执行并留下审计，功能中心只提供能力状态和统计。",
			metrics(
				"consent_event_count", "授权变更", "次",
				"pending_deletion_count", "待注销清理", "个",
			)),
		featureDef("deployment_infrastructure", "部署基础设施", "容器、数据库、缓存和消息服务。", "platform", "平台运维", false,
			nil,
			metrics("running_service_count", "运行服务", "个")),
	}
}

// plannedFeatureDef registers a capability that is designed but not built yet.
//
// A planned feature keeps an honest inventory entry: the console shows it as
// 规划中 and refuses configuration and health probes instead of inventing an
// "active but unknown" status.
func plannedFeatureDef(
	id string,
	name string,
	description string,
	category string,
	owner string,
) Definition {
	return Definition{
		Feature: domain.Feature{
			ID:              id,
			Name:            name,
			Description:     description,
			Category:        category,
			Owner:           owner,
			Status:          domain.FeatureStatusPlanned,
			Health:          domain.HealthUnknown,
			HealthSource:    string(domain.HealthSourceNotConfigured),
			ConfigSchema:    []domain.ConfigField{},
			Values:          map[string]any{},
			ReadOnlyMetrics: []domain.Metric{},
			ReadOnlyReason:  "该功能尚未实现，暂不提供配置和健康检查。",
		},
		ImmutableKeys: map[string]struct{}{},
	}
}

// readOnlyFeatureDef registers an implemented capability whose settings are
// intentionally managed outside the console (for example by deployment
// configuration) or are not yet enforced anywhere.
func readOnlyFeatureDef(
	id string,
	name string,
	description string,
	category string,
	owner string,
	reason string,
	readOnlyMetrics []domain.Metric,
) Definition {
	if readOnlyMetrics == nil {
		readOnlyMetrics = []domain.Metric{}
	}
	if reason == "" {
		reason = "该功能由部署配置管理，控制台只提供状态和指标查看。"
	}
	return Definition{
		Feature: domain.Feature{
			ID:              id,
			Name:            name,
			Description:     description,
			Category:        category,
			Owner:           owner,
			Status:          domain.FeatureStatusActive,
			Health:          domain.HealthUnknown,
			HealthSource:    string(domain.HealthSourceRuntime),
			ConfigSchema:    []domain.ConfigField{},
			Values:          map[string]any{},
			ReadOnlyMetrics: readOnlyMetrics,
			ReadOnlyReason:  reason,
		},
		ImmutableKeys: map[string]struct{}{},
	}
}

func featureDef(
	id string,
	name string,
	description string,
	category string,
	owner string,
	editable bool,
	fields []domain.ConfigField,
	readOnlyMetrics []domain.Metric,
) Definition {
	if fields == nil {
		fields = []domain.ConfigField{}
	}
	if readOnlyMetrics == nil {
		readOnlyMetrics = []domain.Metric{}
	}
	reason := ""
	if !editable {
		reason = "该功能由部署配置管理，控制台只提供状态和指标查看。"
	}
	return Definition{
		Feature: domain.Feature{
			ID:              id,
			Name:            name,
			Description:     description,
			Category:        category,
			Owner:           owner,
			Status:          domain.FeatureStatusActive,
			Health:          domain.HealthUnknown,
			HealthSource:    string(domain.HealthSourceRuntime),
			ConfigSchema:    fields,
			Values:          map[string]any{},
			ReadOnlyMetrics: readOnlyMetrics,
			ReadOnlyReason:  reason,
		},
		ImmutableKeys: map[string]struct{}{},
	}
}

// customMultiselectField allows the operator to select any value that is not
// part of the static option list. It is used for the AI model catalogue, whose
// membership changes as the gateway publishes new models.
func customMultiselectField(
	key string,
	label string,
	value []string,
) domain.ConfigField {
	field := multiselectField(key, label, value, nil, true)
	field.AllowCustom = true
	return field
}

func configFields(fields ...domain.ConfigField) []domain.ConfigField {
	return fields
}

func metrics(entries ...string) []domain.Metric {
	result := make([]domain.Metric, 0, len(entries)/3)
	for index := 0; index+2 < len(entries); index += 3 {
		result = append(result, domain.Metric{
			Key:   entries[index],
			Label: entries[index+1],
			Unit:  entries[index+2],
		})
	}
	return result
}

func systemSelectField(
	key string,
	label string,
	value string,
	options []domain.ConfigOption,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldSelect, value, false)
	field.Options = options
	field.Source = domain.ConfigSourceSystem
	return field
}

func baseField(
	key string,
	label string,
	fieldType domain.ConfigFieldType,
	value any,
	editable bool,
) domain.ConfigField {
	return domain.ConfigField{
		Key:      key,
		Label:    label,
		Type:     fieldType,
		Default:  value,
		Value:    value,
		Source:   domain.ConfigSourceRegistryDefault,
		Editable: editable,
	}
}

func boolField(key string, label string, value bool, editable bool) domain.ConfigField {
	return baseField(key, label, domain.ConfigFieldBoolean, value, editable)
}

func stringField(key string, label string, value string, editable bool) domain.ConfigField {
	return baseField(key, label, domain.ConfigFieldString, value, editable)
}

func integerField(
	key string,
	label string,
	value float64,
	required bool,
	editable bool,
	unit string,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldInteger, value, editable)
	field.Required = required
	field.Unit = unit
	return field
}

func boundedIntegerField(
	key string,
	label string,
	value float64,
	minimum float64,
	maximum float64,
	editable bool,
	unit string,
) domain.ConfigField {
	field := integerField(key, label, value, true, editable, unit)
	field.Min = &minimum
	field.Max = &maximum
	return field
}

func numberField(
	key string,
	label string,
	value float64,
	required bool,
	editable bool,
	unit string,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldNumber, value, editable)
	field.Required = required
	field.Unit = unit
	return field
}

func selectField(
	key string,
	label string,
	value string,
	options []domain.ConfigOption,
	editable bool,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldSelect, value, editable)
	field.Options = options
	return field
}

func multiselectField(
	key string,
	label string,
	value []string,
	options []domain.ConfigOption,
	editable bool,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldMultiselect, value, editable)
	field.Options = options
	return field
}

func durationField(
	key string,
	label string,
	value string,
	required bool,
	editable bool,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldDuration, value, editable)
	field.Required = required
	return field
}

func urlField(
	key string,
	label string,
	value string,
	required bool,
	editable bool,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldURL, value, editable)
	field.Required = required
	return field
}

func secretField(
	key string,
	label string,
	required bool,
	editable bool,
) domain.ConfigField {
	field := baseField(key, label, domain.ConfigFieldSecret, "", editable)
	field.Required = required
	field.Secret = true
	return field
}
