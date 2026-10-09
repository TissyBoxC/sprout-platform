/// Firmware release metadata shown to a guardian before updating a device.
///
/// The contract is deliberately separate from the parent application's own
/// update model: installing a parent-app APK and updating the firmware on a
/// child's device are different actions with different risks.
class DeviceFirmwareRelease {
  const DeviceFirmwareRelease({
    required this.releaseId,
    required this.firmwareVersion,
    required this.hardwareRevision,
    required this.channel,
    required this.status,
    required this.artifactUrl,
    required this.sha256,
    required this.sizeBytes,
    required this.signatureKeyId,
    required this.signature,
    required this.rollbackAllowed,
    required this.minSourceVersion,
    required this.publishedAt,
    required this.canaryPercent,
    required this.targetType,
    required this.targetId,
    required this.releaseNotes,
    this.createdAt,
    this.updatedAt,
  });

  final String releaseId;
  final String firmwareVersion;
  final String hardwareRevision;
  final String channel;
  final String status;
  final String artifactUrl;
  final String sha256;
  final int sizeBytes;
  final String signatureKeyId;
  final String signature;
  final bool rollbackAllowed;
  final String minSourceVersion;
  final DateTime publishedAt;
  final int canaryPercent;
  final String targetType;
  final String targetId;
  final String releaseNotes;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  factory DeviceFirmwareRelease.fromJson(Map<String, Object?> json) {
    return DeviceFirmwareRelease(
      releaseId: _requiredString(json['release_id'], 'release_id'),
      firmwareVersion: _requiredString(
        json['firmware_version'],
        'firmware_version',
      ),
      hardwareRevision: _requiredString(
        json['hardware_revision'],
        'hardware_revision',
      ),
      channel: _requiredChannel(json['channel']),
      status: _requiredReleaseStatus(json['status']),
      artifactUrl: _requiredString(json['artifact_url'], 'artifact_url'),
      sha256: _requiredSha256(json['sha256']),
      sizeBytes: _requiredNonNegativeInt(json['size_bytes'], 'size_bytes'),
      signatureKeyId: _requiredString(
        json['signature_key_id'],
        'signature_key_id',
      ),
      signature: _requiredString(json['signature'], 'signature'),
      rollbackAllowed: json['rollback_allowed'] == true,
      minSourceVersion: _asString(json['min_source_version']),
      publishedAt: _requiredDateTime(json['published_at'], 'published_at'),
      canaryPercent: _boundedPercent(json['canary_percent']),
      targetType: _requiredTargetType(json['target_type']),
      targetId: _asString(json['target_id']),
      releaseNotes: _asString(json['release_notes']),
      createdAt: _asDateTime(json['created_at']),
      updatedAt: _asDateTime(json['updated_at']),
    );
  }

  /// Whether the release metadata is internally safe to offer to a device.
  bool get isInstallable {
    final uri = Uri.tryParse(artifactUrl);
    return status == 'published' &&
        uri != null &&
        uri.scheme == 'https' &&
        uri.host.isNotEmpty &&
        RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(sha256) &&
        sizeBytes > 0 &&
        signature.trim().isNotEmpty &&
        signatureKeyId.trim().isNotEmpty;
  }
}

/// Guardian-facing state of one device's firmware update deployment.
class DeviceFirmwareUpdate {
  const DeviceFirmwareUpdate({
    required this.deviceId,
    required this.status,
    required this.currentVersion,
    required this.updateAvailable,
    required this.rollbackAvailable,
    required this.progressPercent,
    required this.checkedAt,
    this.deploymentId,
    this.targetVersion,
    this.errorCode,
    this.errorMessage,
    this.release,
  });

  final String deviceId;
  final String? deploymentId;
  final DeviceFirmwareUpdateStatus status;
  final String currentVersion;
  final String? targetVersion;
  final bool updateAvailable;
  final bool rollbackAvailable;
  final double progressPercent;
  final String? errorCode;
  final String? errorMessage;
  final DateTime checkedAt;
  final DeviceFirmwareRelease? release;

  factory DeviceFirmwareUpdate.fromJson(Map<String, Object?> json) {
    final releaseValue = json['release'];
    final progressValue = json['progress_percent'];
    return DeviceFirmwareUpdate(
      deviceId: _requiredString(json['device_id'], 'device_id'),
      deploymentId: _nullableTrimmedString(json['deployment_id']),
      status: DeviceFirmwareUpdateStatus.fromWireValue(
        _requiredString(json['status'], 'status'),
      ),
      currentVersion: _requiredString(
        json['current_version'],
        'current_version',
      ),
      targetVersion: _nullableTrimmedString(json['target_version']),
      updateAvailable: json['update_available'] == true,
      rollbackAvailable: json['rollback_available'] == true,
      progressPercent: progressValue is num
          ? progressValue.toDouble().clamp(0, 100)
          : 0,
      errorCode: _nullableTrimmedString(json['error_code']),
      errorMessage: _nullableTrimmedString(json['error_message']),
      checkedAt: _requiredDateTime(json['checked_at'], 'checked_at'),
      release: releaseValue is Map
          ? DeviceFirmwareRelease.fromJson(
              Map<String, Object?>.from(releaseValue),
            )
          : null,
    );
  }

  DeviceFirmwareUpdate copyWith({
    String? deploymentId,
    DeviceFirmwareUpdateStatus? status,
    String? currentVersion,
    String? targetVersion,
    bool? updateAvailable,
    bool? rollbackAvailable,
    double? progressPercent,
    String? errorCode,
    String? errorMessage,
    DateTime? checkedAt,
    DeviceFirmwareRelease? release,
  }) {
    return DeviceFirmwareUpdate(
      deviceId: deviceId,
      deploymentId: deploymentId ?? this.deploymentId,
      status: status ?? this.status,
      currentVersion: currentVersion ?? this.currentVersion,
      targetVersion: targetVersion ?? this.targetVersion,
      updateAvailable: updateAvailable ?? this.updateAvailable,
      rollbackAvailable: rollbackAvailable ?? this.rollbackAvailable,
      progressPercent: progressPercent ?? this.progressPercent,
      errorCode: errorCode ?? this.errorCode,
      errorMessage: errorMessage ?? this.errorMessage,
      checkedAt: checkedAt ?? this.checkedAt,
      release: release ?? this.release,
    );
  }
}

/// Stable device-facing lifecycle values returned by the OTA API.
enum DeviceFirmwareUpdateStatus {
  upToDate('up_to_date'),
  available('available'),
  queued('queued'),
  downloading('downloading'),
  validating('validating'),
  installing('installing'),
  pendingVerify('pending_verify'),
  succeeded('succeeded'),
  failed('failed'),
  rollbackAvailable('rollback_available'),
  rolledBack('rolled_back');

  const DeviceFirmwareUpdateStatus(this.wireValue);

  final String wireValue;

  static DeviceFirmwareUpdateStatus fromWireValue(String value) {
    for (final status in values) {
      if (status.wireValue == value) {
        return status;
      }
    }
    throw FormatException('unknown device firmware status: $value');
  }

  bool get isInProgress => switch (this) {
    queued || downloading || validating || installing || pendingVerify => true,
    _ => false,
  };

  bool get isTerminalFailure => this == failed;

  bool get allowsRetry => this == failed;

  bool get allowsRollback => this == rollbackAvailable;
}

String _requiredString(Object? value, String field) {
  if (value is String && value.trim().isNotEmpty) {
    return value.trim();
  }
  throw FormatException('missing $field');
}

String? _nullableTrimmedString(Object? value) {
  if (value is! String || value.trim().isEmpty) {
    return null;
  }
  return value.trim();
}

String _asString(Object? value) {
  return value is String ? value.trim() : '';
}

int _requiredNonNegativeInt(Object? value, String field) {
  if (value is num && value >= 0) {
    return value.toInt();
  }
  throw FormatException('missing $field');
}

int _boundedPercent(Object? value) {
  if (value is num) {
    return value.toInt().clamp(0, 100);
  }
  return 0;
}

String _requiredSha256(Object? value) {
  final sha256 = _requiredString(value, 'sha256');
  if (!RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(sha256)) {
    throw const FormatException('invalid sha256');
  }
  return sha256;
}

DateTime _requiredDateTime(Object? value, String field) {
  final parsed = _asDateTime(value);
  if (parsed == null) {
    throw FormatException('missing $field');
  }
  return parsed;
}

DateTime? _asDateTime(Object? value) {
  if (value is! String || value.trim().isEmpty) {
    return null;
  }
  return DateTime.tryParse(value.trim());
}

String _requiredChannel(Object? value) {
  final channel = _requiredString(value, 'channel');
  if (!const {'stable', 'canary', 'internal'}.contains(channel)) {
    throw FormatException('unknown channel: $channel');
  }
  return channel;
}

String _requiredReleaseStatus(Object? value) {
  final status = _requiredString(value, 'status');
  if (!const {'draft', 'published', 'paused', 'withdrawn'}.contains(status)) {
    throw FormatException('unknown release status: $status');
  }
  return status;
}

String _requiredTargetType(Object? value) {
  final targetType = _requiredString(value, 'target_type');
  if (!const {'all', 'device', 'group'}.contains(targetType)) {
    throw FormatException('unknown target type: $targetType');
  }
  return targetType;
}
