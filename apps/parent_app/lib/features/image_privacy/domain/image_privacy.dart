/// Guardian-facing image-privacy contract types.
///
/// Only the family policy and bounded processing counters cross this client.
/// Image bytes, thumbnails and face templates never enter the parent app.
library;

/// One validated image-privacy policy for the signed-in family.
class ImagePrivacyPolicy {
  const ImagePrivacyPolicy({
    required this.schemaVersion,
    required this.enabled,
    required this.uploadAllowed,
    required this.localProcessingOnly,
    required this.processingMode,
    required this.uploadScope,
    required this.retentionDays,
    required this.redactionLevel,
    required this.requireChildConsent,
    required this.revision,
  });

  const ImagePrivacyPolicy.conservative()
    : schemaVersion = 1,
      enabled = false,
      uploadAllowed = false,
      localProcessingOnly = true,
      processingMode = 'off',
      uploadScope = 'none',
      retentionDays = 0,
      redactionLevel = 'strict',
      requireChildConsent = true,
      revision = 0;

  final int schemaVersion;
  final bool enabled;
  final bool uploadAllowed;
  final bool localProcessingOnly;
  final String processingMode;
  final String uploadScope;
  final int retentionDays;
  final String redactionLevel;
  final bool requireChildConsent;
  final int revision;

  factory ImagePrivacyPolicy.fromJson(Map<String, Object?> json) {
    return ImagePrivacyPolicy(
      schemaVersion: _int(json['schema_version'], fallback: 1),
      enabled: json['enabled'] == true,
      uploadAllowed: json['upload_allowed'] == true,
      localProcessingOnly: json['local_processing_only'] != false,
      processingMode: json['processing_mode']?.toString() ?? 'off',
      uploadScope: json['upload_scope']?.toString() ?? 'none',
      retentionDays: _int(json['retention_days'], fallback: 0),
      redactionLevel: json['redaction_level']?.toString() ?? 'strict',
      requireChildConsent: json['require_child_consent'] != false,
      revision: _int(json['revision'], fallback: 0),
    );
  }
}

/// Editable policy fields accepted when publishing a new revision.
class ImagePrivacyDraft {
  const ImagePrivacyDraft({
    required this.enabled,
    required this.uploadAllowed,
    required this.localProcessingOnly,
    required this.processingMode,
    required this.uploadScope,
    required this.retentionDays,
    required this.redactionLevel,
    required this.requireChildConsent,
  });

  final bool enabled;
  final bool uploadAllowed;
  final bool localProcessingOnly;
  final String processingMode;
  final String uploadScope;
  final int retentionDays;
  final String redactionLevel;
  final bool requireChildConsent;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'upload_allowed': uploadAllowed,
      'local_processing_only': localProcessingOnly,
      'processing_mode': processingMode,
      'upload_scope': uploadScope,
      'retention_days': retentionDays,
      'redaction_level': redactionLevel,
      'require_child_consent': requireChildConsent,
    };
  }
}

/// Wire catalogs returned by the policy endpoint for editor controls.
class ImagePrivacyCatalogs {
  const ImagePrivacyCatalogs({
    required this.processingModes,
    required this.uploadScopes,
    required this.redactionLevels,
    required this.blockReasons,
  });

  const ImagePrivacyCatalogs.conservative()
    : processingModes = const <String>['off', 'metadata', 'local_full'],
      uploadScopes = const <String>['none', 'thumbnails', 'analysis'],
      redactionLevels = const <String>['none', 'standard', 'strict'],
      blockReasons = const <String>[
        'none',
        'disabled',
        'consent',
        'scope',
        'quota',
        'error',
      ];

  final List<String> processingModes;
  final List<String> uploadScopes;
  final List<String> redactionLevels;
  final List<String> blockReasons;

  factory ImagePrivacyCatalogs.fromJson(Map<String, Object?> json) {
    return ImagePrivacyCatalogs(
      processingModes: _strings(json['processing_modes'], const <String>[
        'off',
        'metadata',
        'local_full',
      ]),
      uploadScopes: _strings(json['upload_scopes'], const <String>[
        'none',
        'thumbnails',
        'analysis',
      ]),
      redactionLevels: _strings(json['redaction_levels'], const <String>[
        'none',
        'standard',
        'strict',
      ]),
      blockReasons: _strings(json['block_reasons'], const <String>[
        'none',
        'disabled',
        'consent',
        'scope',
        'quota',
        'error',
      ]),
    );
  }
}

/// One policy read paired with its safe editor catalogs.
class ImagePrivacyConfig {
  const ImagePrivacyConfig({required this.policy, required this.catalogs});

  final ImagePrivacyPolicy policy;
  final ImagePrivacyCatalogs catalogs;
}

/// One device's latest bounded image-processing status.
class ImagePrivacyDevice {
  const ImagePrivacyDevice({
    required this.deviceId,
    required this.cameraSupported,
    required this.localProcessing,
    required this.uploadAllowed,
    required this.processingMode,
    required this.uploadScope,
    required this.redactionLevel,
    required this.processedCount,
    required this.uploadedCount,
    required this.blockedCount,
    required this.lastBlockReason,
    required this.upToDate,
  });

  final String deviceId;
  final bool cameraSupported;
  final bool localProcessing;
  final bool uploadAllowed;
  final String processingMode;
  final String uploadScope;
  final String redactionLevel;
  final int processedCount;
  final int uploadedCount;
  final int blockedCount;
  final String lastBlockReason;
  final bool upToDate;

  bool get blocked => blockedCount > 0;

  factory ImagePrivacyDevice.fromJson(Map<String, Object?> json) {
    return ImagePrivacyDevice(
      deviceId: json['device_id']?.toString() ?? '',
      cameraSupported: json['camera_supported'] == true,
      localProcessing: json['local_processing'] == true,
      uploadAllowed: json['upload_allowed'] == true,
      processingMode: json['processing_mode']?.toString() ?? 'off',
      uploadScope: json['upload_scope']?.toString() ?? 'none',
      redactionLevel: json['redaction_level']?.toString() ?? 'strict',
      processedCount: _int(json['processed_count'], fallback: 0),
      uploadedCount: _int(json['uploaded_count'], fallback: 0),
      blockedCount: _int(json['blocked_count'], fallback: 0),
      lastBlockReason: json['last_block_reason']?.toString() ?? 'none',
      upToDate: json['up_to_date'] == true,
    );
  }
}

/// One device's current image-privacy capability and applied revision.
class ImagePrivacyCapability {
  const ImagePrivacyCapability({
    required this.supported,
    required this.localProcessing,
    required this.uploadAllowed,
    required this.processingMode,
    required this.uploadScope,
    required this.redactionLevel,
    required this.schemaVersion,
    required this.policyRevision,
    required this.appliedRevision,
    required this.upToDate,
  });

  final bool supported;
  final bool localProcessing;
  final bool uploadAllowed;
  final String processingMode;
  final String uploadScope;
  final String redactionLevel;
  final int schemaVersion;
  final int policyRevision;
  final int appliedRevision;
  final bool upToDate;

  factory ImagePrivacyCapability.fromJson(Map<String, Object?> json) {
    return ImagePrivacyCapability(
      supported: json['supported'] == true,
      localProcessing: json['local_processing'] == true,
      uploadAllowed: json['upload_allowed'] == true,
      processingMode: json['processing_mode']?.toString() ?? 'off',
      uploadScope: json['upload_scope']?.toString() ?? 'none',
      redactionLevel: json['redaction_level']?.toString() ?? 'strict',
      schemaVersion: _int(json['schema_version'], fallback: 1),
      policyRevision: _int(json['policy_revision'], fallback: 0),
      appliedRevision: _int(json['applied_revision'], fallback: 0),
      upToDate: json['up_to_date'] == true,
    );
  }
}

/// Stable Chinese label for a processing mode.
String imageProcessingModeLabel(String mode) {
  return switch (mode) {
    'off' => '关闭',
    'metadata' => '元数据处理',
    'local_full' => '本地完整处理',
    _ => mode,
  };
}

/// Stable Chinese label for an upload scope.
String imageUploadScopeLabel(String scope) {
  return switch (scope) {
    'none' => '不上传',
    'thumbnails' => '仅缩略图',
    'analysis' => '分析数据',
    _ => scope,
  };
}

/// Stable Chinese label for a redaction level.
String imageRedactionLevelLabel(String level) {
  return switch (level) {
    'none' => '不处理',
    'standard' => '标准',
    'strict' => '严格',
    _ => level,
  };
}

/// Stable Chinese label for a device block reason.
String imageBlockReasonLabel(String reason) {
  return switch (reason) {
    'none' => '无',
    'disabled' => '功能未开启',
    'consent' => '等待监护授权',
    'scope' => '超出上传范围',
    'quota' => '达到用量上限',
    'error' => '处理异常',
    _ => '未知',
  };
}

int _int(Object? value, {required int fallback}) {
  if (value is num) {
    return value.toInt();
  }
  return fallback;
}

List<String> _strings(Object? value, List<String> fallback) {
  if (value is! List) {
    return fallback;
  }
  final strings = value
      .whereType<String>()
      .where((item) => item.trim().isNotEmpty)
      .toList(growable: false);
  return strings.isEmpty ? fallback : strings;
}
