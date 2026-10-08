/// Guardian consent and account lifecycle state for privacy controls.
class PrivacyStatus {
  const PrivacyStatus({
    required this.consent,
    required this.deletion,
    required this.exportAvailable,
    required this.policy,
  });

  final GuardianConsent consent;
  final PrivacyDeletion deletion;
  final bool exportAvailable;
  final PrivacyRetentionPolicy policy;

  factory PrivacyStatus.fromJson(Map<String, Object?> json) {
    return PrivacyStatus(
      consent: GuardianConsent.fromJson(
        _requiredMap(json['consent'], 'consent'),
      ),
      deletion: PrivacyDeletion.fromJson(
        _requiredMap(json['deletion'], 'deletion'),
      ),
      exportAvailable: json['export'] is Map
          ? _map(json['export'])['available'] == true
          : false,
      policy: PrivacyRetentionPolicy.fromJson(
        _requiredMap(json['policy'], 'policy'),
      ),
    );
  }

  PrivacyStatus copyWith({
    GuardianConsent? consent,
    PrivacyDeletion? deletion,
    bool? exportAvailable,
    PrivacyRetentionPolicy? policy,
  }) {
    return PrivacyStatus(
      consent: consent ?? this.consent,
      deletion: deletion ?? this.deletion,
      exportAvailable: exportAvailable ?? this.exportAvailable,
      policy: policy ?? this.policy,
    );
  }
}

enum GuardianConsentStatus { active, withdrawn }

/// The guardian's current consent version and withdrawal state.
class GuardianConsent {
  const GuardianConsent({
    required this.version,
    required this.consentedAt,
    required this.withdrawnAt,
    required this.status,
    this.reconsentPath,
  });

  final String version;
  final DateTime? consentedAt;
  final DateTime? withdrawnAt;
  final GuardianConsentStatus status;

  /// In-app path supplied by the platform for restoring consent.
  ///
  /// The client never guesses this route: it is only actionable when the
  /// server returns a safe, app-local path.
  final String? reconsentPath;

  bool get isActive => status == GuardianConsentStatus.active;

  /// A safe, app-local route that can be passed to the application router.
  String? get reconsentRoute {
    if (isActive || reconsentPath == null) {
      return null;
    }
    final uri = Uri.tryParse(reconsentPath!);
    if (uri == null ||
        uri.hasScheme ||
        uri.host.isNotEmpty ||
        !uri.path.startsWith('/')) {
      return null;
    }
    return uri.hasQuery ? '${uri.path}?${uri.query}' : uri.path;
  }

  bool get canReauthorize => reconsentRoute != null;

  factory GuardianConsent.fromJson(Map<String, Object?> json) {
    final status = switch (_optionalString(json['status'])) {
      'withdrawn' => GuardianConsentStatus.withdrawn,
      'active' => GuardianConsentStatus.active,
      _ =>
        json['active'] == false
            ? GuardianConsentStatus.withdrawn
            : GuardianConsentStatus.active,
    };
    return GuardianConsent(
      version: _firstString(json, const [
        'guardian_consent_version',
        'version',
      ]),
      consentedAt: _optionalDateTime(
        json['guardian_consented_at'] ?? json['consented_at'],
      ),
      withdrawnAt: _optionalDateTime(json['withdrawn_at']),
      status: status,
      reconsentPath: _optionalNullableString(json['reconsent_path']),
    );
  }
}

enum PrivacyDeletionStatus { none, pending, completed }

/// Account deletion request state, including the cancellable waiting period.
class PrivacyDeletion {
  const PrivacyDeletion({
    required this.status,
    required this.requestedAt,
    required this.scheduledFor,
    required this.completedAt,
    required this.cancellable,
  });

  const PrivacyDeletion.none()
    : status = PrivacyDeletionStatus.none,
      requestedAt = null,
      scheduledFor = null,
      completedAt = null,
      cancellable = false;

  final PrivacyDeletionStatus status;
  final DateTime? requestedAt;
  final DateTime? scheduledFor;
  final DateTime? completedAt;
  final bool cancellable;

  bool get isNone => status == PrivacyDeletionStatus.none;
  bool get isPending => status == PrivacyDeletionStatus.pending;
  bool get isCompleted => status == PrivacyDeletionStatus.completed;

  factory PrivacyDeletion.fromJson(Map<String, Object?> json) {
    final status = switch (_optionalString(json['status'])) {
      'pending' => PrivacyDeletionStatus.pending,
      'completed' => PrivacyDeletionStatus.completed,
      _ => PrivacyDeletionStatus.none,
    };
    return PrivacyDeletion(
      status: status,
      requestedAt: _optionalDateTime(json['requested_at']),
      scheduledFor: _optionalDateTime(json['scheduled_for']),
      completedAt: _optionalDateTime(json['completed_at']),
      cancellable: json['cancellable'] == true,
    );
  }
}

/// Data retention limits disclosed to the guardian.
class PrivacyRetentionPolicy {
  const PrivacyRetentionPolicy({
    required this.audioRetentionDays,
    required this.imageRetentionDays,
    required this.conversationRetentionDays,
  });

  final int audioRetentionDays;
  final int imageRetentionDays;
  final int conversationRetentionDays;

  factory PrivacyRetentionPolicy.fromJson(Map<String, Object?> json) {
    return PrivacyRetentionPolicy(
      audioRetentionDays: _requiredInt(
        json['audio_retention_days'],
        'audio_retention_days',
      ),
      imageRetentionDays: _requiredInt(
        json['image_retention_days'],
        'image_retention_days',
      ),
      conversationRetentionDays: _requiredInt(
        json['conversation_retention_days'],
        'conversation_retention_days',
      ),
    );
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}

Map<String, Object?> _map(Object? value) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  return const <String, Object?>{};
}

String _optionalString(Object? value) {
  return value is String ? value : '';
}

String _firstString(Map<String, Object?> json, List<String> keys) {
  for (final key in keys) {
    final value = json[key];
    if (value is String && value.isNotEmpty) {
      return value;
    }
  }
  return '';
}

String? _optionalNullableString(Object? value) {
  if (value is! String) {
    return null;
  }
  final trimmed = value.trim();
  return trimmed.isEmpty ? null : trimmed;
}

DateTime? _optionalDateTime(Object? value) {
  if (value is! String || value.isEmpty) {
    return null;
  }
  return DateTime.tryParse(value)?.toLocal();
}

int _requiredInt(Object? value, String field) {
  if (value is num) {
    return value.toInt();
  }
  throw FormatException('missing $field');
}
