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
		featureDef("parent_account", "家长账号", "家长资料、密码与账号状态管理。", "identity", "账号平台", true,
			nil,
			metrics("active_parent_count", "有效家长数", "个")),
		featureDef("ai_account", "AI 账号", "家长 AI 账号的额度、并发与模型配置。", "ai", "AI 服务", true,
			configFields(
				numberField("default_balance_usd", "默认额度", float64(0), true, true, ""),
				integerField("default_concurrency", "默认并发", float64(1), true, true, "路"),
				customMultiselectField("default_models", "默认模型", []string{}),
			),
			metrics("active_ai_account_count", "有效 AI 账号", "个")),
		featureDef("child_profile", "儿童档案", "儿童昵称、生日和年龄分级资料。", "family", "家庭平台", true,
			nil,
			metrics("child_profile_count", "儿童档案数", "个")),
		featureDef("parent_policy", "家长策略", "内容时长、分类和禁用时段策略。", "family", "家庭平台", true,
			nil,
			metrics("policy_update_count", "策略更新数", "次")),
		featureDef("device_binding", "设备绑定", "设备注册、认证和家庭绑定关系。", "device", "设备平台", true,
			nil,
			metrics("bound_device_count", "已绑定设备", "台")),
		featureDef("device_runtime", "设备运行", "在线状态、心跳和远程指令。", "device", "设备平台", true,
			nil,
			metrics("online_device_count", "在线设备", "台")),
		featureDef("device_diagnostics", "设备诊断", "设备运行日志、配网和故障快照。", "device", "设备平台", true,
			nil,
			metrics("diagnostic_event_count", "诊断事件", "条")),
		featureDef("content_library", "内容库", "儿童内容包的审核、发布与下发。", "content", "内容平台", true,
			nil,
			metrics("published_content_count", "已发布内容", "个")),
		featureDef("ota_release", "设备更新", "设备固件和应用更新发布。", "release", "发布平台", true,
			configFields(
				selectField("default_channel", "默认渠道", "stable", []domain.ConfigOption{
					{Value: "stable", Label: "稳定版"},
					{Value: "beta", Label: "测试版"},
					{Value: "canary", Label: "灰度版"},
				}, true),
				stringField("minimum_client_version", "最低客户端版本", "", true),
				stringField("mandatory_update_threshold", "强制更新线", "", true),
			),
			metrics("published_firmware_count", "已发布版本", "个")),
		featureDef("download_server", "下载服务", "内容、更新和应用文件的分发。", "release", "发布平台", false,
			nil,
			metrics("download_file_count", "发布文件", "个")),
		featureDef("service_version", "服务版本", "平台服务版本的检查和升级。", "platform", "平台运维", true,
			nil,
			metrics("outdated_service_count", "待升级服务", "个")),
		plannedFeatureDef("ui_text", "界面文案", "客户端和管理端的文案配置。", "content", "内容平台"),
		featureDef("voice_gateway", "语音网关", "儿童实时语音会话和语音安全。", "voice", "语音平台", true,
			nil,
			metrics("active_voice_session_count", "实时会话", "路")),
		featureDef("ai_model_gateway", "模型网关", "模型路由、限额与供应商健康。", "ai", "AI 服务", true,
			nil,
			metrics("model_request_count", "模型调用", "次")),
		readOnlyFeatureDef("usage_report", "使用报告", "设备使用时长和内容统计。", "family", "家庭平台",
			"数据保留天数尚未接入自动清理任务，接入后才会开放修改。",
			metrics("reported_usage_days", "已上报天数", "天")),
		plannedFeatureDef("notification", "消息通知", "家长通知、短信和消息触达。", "platform", "平台运维"),
		featureDef("platform_security", "平台安全", "管理员权限、审计与安全策略。", "security", "安全团队", true,
			configFields(
				boolField("minor_mode_default", "默认启用未成年人模式", true, true),
				boolField("output_moderation_enabled", "启用输出审核", true, true),
				boolField("crisis_intervention_enabled", "启用危机干预", true, true),
			),
			metrics("blocked_login_count", "已阻止登录", "次")),
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
