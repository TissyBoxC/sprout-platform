/// Guardian-facing camera and vision contract types.
///
/// The platform stores only bounded, symbolic task metadata and recognized
/// labels. Image bytes are never returned to this client.
library;

/// One bounded vision task for a device.
class VisionTask {
  const VisionTask({
    required this.taskId,
    required this.deviceId,
    required this.taskType,
    required this.status,
    required this.prompt,
    required this.failureCode,
    required this.createdAt,
    required this.expiresAt,
  });

  final String taskId;
  final String deviceId;
  final String taskType;
  final String status;
  final String prompt;
  final String failureCode;
  final DateTime? createdAt;
  final DateTime? expiresAt;

  bool get isPending => status == 'pending' || status == 'dispatched';
  bool get isCompleted => status == 'completed';

  factory VisionTask.fromJson(Map<String, Object?> json) {
    return VisionTask(
      taskId: _asString(json['task_id']),
      deviceId: _asString(json['device_id']),
      taskType: _asString(json['task_type']),
      status: _asString(json['status']),
      prompt: _asString(json['prompt']),
      failureCode: _asString(json['failure_code']),
      createdAt: _asDateTime(json['created_at']),
      expiresAt: _asDateTime(json['expires_at']),
    );
  }
}

/// One recognized label with an integer confidence score (0..10000).
class VisionLabel {
  const VisionLabel({required this.label, required this.confidence});

  final String label;
  final int confidence;

  factory VisionLabel.fromJson(Map<String, Object?> json) {
    return VisionLabel(
      label: _asString(json['label']),
      confidence: _asInt(json['confidence']),
    );
  }
}

/// One short-lived recognition result.
class VisionResult {
  const VisionResult({
    required this.taskId,
    required this.deviceId,
    required this.taskType,
    required this.labels,
    required this.summary,
    required this.createdAt,
    required this.expiresAt,
  });

  final String taskId;
  final String deviceId;
  final String taskType;
  final List<VisionLabel> labels;
  final String summary;
  final DateTime? createdAt;
  final DateTime? expiresAt;

  factory VisionResult.fromJson(Map<String, Object?> json) {
    final labelsValue = json['labels'];
    return VisionResult(
      taskId: _asString(json['task_id']),
      deviceId: _asString(json['device_id']),
      taskType: _asString(json['task_type']),
      labels: labelsValue is List
          ? labelsValue
                .whereType<Map>()
                .map(
                  (item) => VisionLabel.fromJson(
                    Map<String, Object?>.from(item),
                  ),
                )
                .toList(growable: false)
          : const <VisionLabel>[],
      summary: _asString(json['summary']),
      createdAt: _asDateTime(json['created_at']),
      expiresAt: _asDateTime(json['expires_at']),
    );
  }
}

/// Camera capability state for one device.
class VisionCapability {
  const VisionCapability({
    required this.supported,
    required this.consentGranted,
    required this.reconsentPath,
    required this.retentionHours,
  });

  final bool supported;
  final bool consentGranted;
  final String reconsentPath;
  final int retentionHours;

  factory VisionCapability.fromJson(Map<String, Object?> json) {
    return VisionCapability(
      supported: json['supported'] == true,
      consentGranted: json['consent_granted'] == true,
      reconsentPath: _asString(json['reconsent_path']),
      retentionHours: _asInt(json['retention_hours']),
    );
  }
}

String visionTaskTypeLabel(String taskType) {
  return switch (taskType) {
    'object_recognition' => '识物',
    'picture_book_recognition' => '绘本识别',
    'photo_qa' => '拍照问答',
    'capture' => '拍照',
    _ => '视觉任务',
  };
}

String visionStatusLabel(String status) {
  return switch (status) {
    'pending' => '等待设备',
    'dispatched' => '设备处理中',
    'completed' => '已完成',
    'failed' => '未完成',
    'cancelled' => '已取消',
    'expired' => '已超时',
    _ => '状态未知',
  };
}

String _asString(Object? value) => value is String ? value : '';

int _asInt(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return 0;
}

DateTime? _asDateTime(Object? value) {
  if (value is String) {
    return DateTime.tryParse(value);
  }
  return null;
}
