/// Firmware release metadata shown to a guardian before updating a device.
///
/// The contract is deliberately separate from the parent application's own
/// update model: installing a parent-app APK and updating the firmware on a
/// child's device are different actions with different risks.
class DeviceFirmwareRelease {
  const DeviceFirmwareRelease({
    this.schemaVersion = '1.0.0',
    required this.releaseId,
    required this.firmwareVersion,
    required this.hardwareRevision,
    required this.channel,
    required this.status,
    required this.artifactKey,
    required this.artifactUrl,
    required this.sha256,
    required this.sizeBytes,
    required this.signatureKeyId,
    required this.signature,
    required this.rollbackAllowed,
    required this.minSourceVersion,
    required this.publishedAt,
    this.canaryPercent,
    this.targetType,
    this.targetId,
    this.releaseNotes,
    this.createdAt,
    this.updatedAt,
  });

  final String schemaVersion;
  final String releaseId;
  final String firmwareVersion;
  final String hardwareRevision;
  final String channel;
  final String status;
  final String artifactKey;
  final String artifactUrl;
  final String sha256;
  final int sizeBytes;
  final String signatureKeyId;
  final String signature;
  final bool rollbackAllowed;
  final String minSourceVersion;
  final DateTime publishedAt;
  final int? canaryPercent;
  final String? targetType;
  final String? targetId;
  final String? releaseNotes;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  factory DeviceFirmwareRelease.fromJson(Map<String, Object?> json) {
    return DeviceFirmwareRelease(
      schemaVersion: _requiredSchemaVersion(json['schema_version']),
      releaseId: _requiredString(json['release_id'], 'release_id'),
      firmwareVersion: _requiredFirmwareVersion(json),
      hardwareRevision: _requiredString(
        json['hardware_revision'],
        'hardware_revision',
      ),
      channel: _requiredChannel(json['channel']),
      status: _requiredReleaseStatus(json['status']),
      artifactKey: _requiredString(json['artifact_key'], 'artifact_key'),
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
      canaryPercent: _optionalPercent(json['canary_percent']),
      targetType: _optionalTargetType(json['target_type']),
      targetId: _nullableTrimmedString(json['target_id']),
      releaseNotes: _nullableTrimmedString(json['release_notes']),
      createdAt: _asDateTime(json['created_at']),
      updatedAt: _asDateTime(json['updated_at']),
    );
  }

  /// Whether the release manifest is complete enough to offer to a device.
  ///
  /// Cryptographic proof is always the platform-verified detached signature;
  /// this model never treats the checksum as a signature.
  bool get isInstallable {
    final uri = Uri.tryParse(artifactUrl);
    return status == 'published' &&
        artifactKey.trim().isNotEmpty &&
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
    this.schemaVersion = '1.0.0',
    required this.deviceId,
    required this.status,
    required this.currentVersion,
    required this.updateAvailable,
    required this.progressPercent,
    required this.checkedAt,
    this.deploymentId,
    this.deploymentKind,
    this.targetVersion,
    this.rollbackReleaseId,
    this.rollbackOfDeploymentId,
    this.errorCode,
    this.errorMessage,
    this.release,
  });

  final String schemaVersion;
  final String deviceId;
  final String? deploymentId;
  final DeviceFirmwareDeploymentKind? deploymentKind;
  final DeviceFirmwareUpdateStatus status;
  final String currentVersion;
  final String? targetVersion;
  final bool updateAvailable;
  bool get rollbackAvailable =>
      status == DeviceFirmwareUpdateStatus.rollbackAvailable &&
      rollbackReleaseId != null &&
      rollbackOfDeploymentId != null;
  final String? rollbackReleaseId;
  final String? rollbackOfDeploymentId;
  final double progressPercent;
  final String? errorCode;
  final String? errorMessage;
  final DateTime checkedAt;
  final DeviceFirmwareRelease? release;

  factory DeviceFirmwareUpdate.fromJson(Map<String, Object?> json) {
    final releaseValue = json['release'];
    final progressValue = json['progress_percent'];
    final deploymentId = _nullableTrimmedString(json['deployment_id']);
    final deploymentKind = _optionalDeploymentKind(json['deployment_kind']);
    final rollbackReleaseId = _nullableTrimmedString(
      json['rollback_release_id'],
    );
    final rollbackOfDeploymentId = _nullableTrimmedString(
      json['rollback_of_deployment_id'],
    );
    final status = DeviceFirmwareUpdateStatus.fromWireValue(
      _requiredString(json['status'], 'status'),
    );
    final wireUpdateAvailable = _requiredBool(
      json['update_available'],
      'update_available',
    );
    final wireRollbackAvailable = _requiredBool(
      json['rollback_available'],
      'rollback_available',
    );
    final expectedRollbackAvailable =
        status == DeviceFirmwareUpdateStatus.rollbackAvailable &&
        rollbackReleaseId != null &&
        rollbackOfDeploymentId != null;
    if (wireRollbackAvailable != expectedRollbackAvailable) {
      throw const FormatException(
        'rollback availability conflicts with status',
      );
    }
    final release = releaseValue is Map
        ? DeviceFirmwareRelease.fromJson(
            Map<String, Object?>.from(releaseValue),
          )
        : null;
    _validateRollbackContract(
      deploymentId: deploymentId,
      deploymentKind: deploymentKind,
      rollbackReleaseId: rollbackReleaseId,
      rollbackOfDeploymentId: rollbackOfDeploymentId,
      status: status,
      updateAvailable: wireUpdateAvailable,
      rollbackAvailable: wireRollbackAvailable,
      release: release,
    );
    return DeviceFirmwareUpdate(
      schemaVersion: _requiredSchemaVersion(json['schema_version']),
      deviceId: _requiredString(json['device_id'], 'device_id'),
      deploymentId: deploymentId,
      deploymentKind: deploymentKind,
      status: status,
      currentVersion: _requiredString(
        json['current_version'],
        'current_version',
      ),
      targetVersion: _nullableTrimmedString(json['target_version']),
      updateAvailable: wireUpdateAvailable,
      rollbackReleaseId: rollbackReleaseId,
      rollbackOfDeploymentId: rollbackOfDeploymentId,
      progressPercent: progressValue is num
          ? progressValue.toDouble().clamp(0, 100)
          : 0,
      errorCode: _nullableTrimmedString(json['error_code']),
      errorMessage: _nullableTrimmedString(json['error_message']),
      checkedAt: _requiredDateTime(json['checked_at'], 'checked_at'),
      release: release,
    );
  }

  /// Whether this deployment is an authorized firmware rollback.
  bool get isAuthorizedRollback =>
      deploymentKind == DeviceFirmwareDeploymentKind.rollback;

  /// Why the current rollback controls are unavailable, when applicable.
  String? get rollbackBlockedReason {
    if (rollbackReleaseId == null || rollbackOfDeploymentId == null) {
      return '当前没有可回退的已授权版本';
    }
    if (status.isInProgress) {
      return '固件正在处理中，完成前不能回退';
    }
    return null;
  }

  DeviceFirmwareUpdate copyWith({
    String? deploymentId,
    DeviceFirmwareDeploymentKind? deploymentKind,
    DeviceFirmwareUpdateStatus? status,
    String? currentVersion,
    String? targetVersion,
    bool? updateAvailable,
    String? rollbackReleaseId,
    String? rollbackOfDeploymentId,
    double? progressPercent,
    String? errorCode,
    String? errorMessage,
    DateTime? checkedAt,
    DeviceFirmwareRelease? release,
  }) {
    return DeviceFirmwareUpdate(
      schemaVersion: schemaVersion,
      deviceId: deviceId,
      deploymentId: deploymentId ?? this.deploymentId,
      deploymentKind: deploymentKind ?? this.deploymentKind,
      status: status ?? this.status,
      currentVersion: currentVersion ?? this.currentVersion,
      targetVersion: targetVersion ?? this.targetVersion,
      updateAvailable: updateAvailable ?? this.updateAvailable,
      rollbackReleaseId: rollbackReleaseId ?? this.rollbackReleaseId,
      rollbackOfDeploymentId:
          rollbackOfDeploymentId ?? this.rollbackOfDeploymentId,
      progressPercent: progressPercent ?? this.progressPercent,
      errorCode: errorCode ?? this.errorCode,
      errorMessage: errorMessage ?? this.errorMessage,
      checkedAt: checkedAt ?? this.checkedAt,
      release: release ?? this.release,
    );
  }
}

/// How the platform created the current firmware deployment.
enum DeviceFirmwareDeploymentKind {
  install('install'),
  rollback('rollback');

  const DeviceFirmwareDeploymentKind(this.wireValue);

  final String wireValue;

  static DeviceFirmwareDeploymentKind fromWireValue(String value) {
    for (final kind in values) {
      if (kind.wireValue == value) {
        return kind;
      }
    }
    throw FormatException('unknown device firmware deployment kind: $value');
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

/// Canonical releases use `firmware_version`. A small number of platform
/// responses during the contract migration still expose `version`; accepting
/// it here keeps display and parsing compatible without weakening signature or
/// checksum validation.
String _requiredFirmwareVersion(Map<String, Object?> json) {
  final canonical = json['firmware_version'];
  if (canonical is String && canonical.trim().isNotEmpty) {
    return canonical.trim();
  }
  final legacy = json['version'];
  if (legacy is String && legacy.trim().isNotEmpty) {
    return legacy.trim();
  }
  throw const FormatException('missing firmware_version');
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

bool _requiredBool(Object? value, String field) {
  if (value is bool) {
    return value;
  }
  throw FormatException('missing $field');
}

int? _optionalPercent(Object? value) {
  if (value == null) {
    return null;
  }
  if (value is int && value >= 0 && value <= 100) {
    return value;
  }
  throw const FormatException('invalid percent');
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

String _requiredSchemaVersion(Object? value) {
  final schemaVersion = _requiredString(value, 'schema_version');
  if (schemaVersion != '1.0.0') {
    throw FormatException('unsupported schema version: $schemaVersion');
  }
  return schemaVersion;
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

String? _optionalTargetType(Object? value) {
  final targetType = _nullableTrimmedString(value);
  if (targetType == null) {
    return null;
  }
  if (!const {'all', 'device', 'group'}.contains(targetType)) {
    throw FormatException('unknown target type: $targetType');
  }
  return targetType;
}

DeviceFirmwareDeploymentKind? _optionalDeploymentKind(Object? value) {
  final kind = _nullableTrimmedString(value);
  if (kind == null) {
    return null;
  }
  return DeviceFirmwareDeploymentKind.fromWireValue(kind);
}

void _validateRollbackContract({
  required String? deploymentId,
  required DeviceFirmwareDeploymentKind? deploymentKind,
  required String? rollbackReleaseId,
  required String? rollbackOfDeploymentId,
  required DeviceFirmwareUpdateStatus status,
  required bool updateAvailable,
  required bool rollbackAvailable,
  required DeviceFirmwareRelease? release,
}) {
  final hasRollbackIdentity =
      rollbackReleaseId != null || rollbackOfDeploymentId != null;

  if (deploymentKind == null) {
    // Older platform versions may omit the new field. The rollback IDs and
    // the explicit rollback status remain authoritative when present.
    if (status == DeviceFirmwareUpdateStatus.rollbackAvailable &&
        !rollbackAvailable) {
      throw const FormatException('rollback status without availability');
    }
    if (hasRollbackIdentity) {
      if (rollbackReleaseId == null || rollbackOfDeploymentId == null) {
        throw const FormatException('incomplete rollback identity');
      }
      if (release == null || release.releaseId != rollbackReleaseId) {
        throw const FormatException('rollback release mismatch');
      }
      if (deploymentId == null) {
        throw const FormatException('rollback without deployment id');
      }
      if (!rollbackAvailable ||
          !updateAvailable ||
          status != DeviceFirmwareUpdateStatus.rollbackAvailable) {
        throw const FormatException('rollback identity outside rollback state');
      }
    }
    return;
  }

  if (deploymentId == null) {
    throw const FormatException('deployment kind without deployment id');
  }

  switch (deploymentKind) {
    case DeviceFirmwareDeploymentKind.install:
      if (hasRollbackIdentity ||
          rollbackAvailable ||
          status == DeviceFirmwareUpdateStatus.rollbackAvailable) {
        throw const FormatException('install deployment marked as rollback');
      }
    case DeviceFirmwareDeploymentKind.rollback:
      if (rollbackReleaseId == null || rollbackOfDeploymentId == null) {
        throw const FormatException('rollback deployment is incomplete');
      }
      if (release == null || release.releaseId != rollbackReleaseId) {
        throw const FormatException('rollback release mismatch');
      }
      if (!updateAvailable) {
        throw const FormatException('rollback deployment is unavailable');
      }
      if (status == DeviceFirmwareUpdateStatus.rolledBack) {
        if (rollbackAvailable) {
          throw const FormatException('completed rollback still available');
        }
        return;
      }
      if (!rollbackAvailable ||
          status != DeviceFirmwareUpdateStatus.rollbackAvailable) {
        throw const FormatException('rollback deployment state mismatch');
      }
  }
}
