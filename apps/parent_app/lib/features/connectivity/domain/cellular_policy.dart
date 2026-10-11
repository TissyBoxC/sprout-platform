/// Guardian-facing cellular policy contract types.
///
/// The platform stores only bounded policy scalars and bounded device
/// connectivity status; subscriber identifiers and call detail never flow
/// through this client.
library;

/// The preferred radio the device attaches to.
enum RadioPreference { wifiFirst, cellularFirst, cellularOnly }

RadioPreference radioPreferenceFromWire(String value) {
  switch (value) {
    case 'cellular_first':
      return RadioPreference.cellularFirst;
    case 'cellular_only':
      return RadioPreference.cellularOnly;
    case 'wifi_first':
    default:
      return RadioPreference.wifiFirst;
  }
}

String radioPreferenceToWire(RadioPreference value) {
  switch (value) {
    case RadioPreference.cellularFirst:
      return 'cellular_first';
    case RadioPreference.cellularOnly:
      return 'cellular_only';
    case RadioPreference.wifiFirst:
      return 'wifi_first';
  }
}

/// One validated cellular policy for the family.
class CellularPolicy {
  const CellularPolicy({
    required this.enabled,
    required this.roamingAllowed,
    required this.remoteWakeEnabled,
    required this.meteredUploadLimit,
    required this.preference,
    required this.monthlyQuotaMb,
    required this.lowQuotaWarnPercent,
    required this.keepaliveSeconds,
    required this.revision,
  });

  final bool enabled;
  final bool roamingAllowed;
  final bool remoteWakeEnabled;
  final bool meteredUploadLimit;
  final RadioPreference preference;
  final int monthlyQuotaMb;
  final int lowQuotaWarnPercent;
  final int keepaliveSeconds;
  final int revision;

  CellularPolicy copyWith({
    bool? enabled,
    bool? roamingAllowed,
    bool? remoteWakeEnabled,
    bool? meteredUploadLimit,
    RadioPreference? preference,
    int? monthlyQuotaMb,
    int? lowQuotaWarnPercent,
    int? keepaliveSeconds,
  }) {
    return CellularPolicy(
      enabled: enabled ?? this.enabled,
      roamingAllowed: roamingAllowed ?? this.roamingAllowed,
      remoteWakeEnabled: remoteWakeEnabled ?? this.remoteWakeEnabled,
      meteredUploadLimit: meteredUploadLimit ?? this.meteredUploadLimit,
      preference: preference ?? this.preference,
      monthlyQuotaMb: monthlyQuotaMb ?? this.monthlyQuotaMb,
      lowQuotaWarnPercent: lowQuotaWarnPercent ?? this.lowQuotaWarnPercent,
      keepaliveSeconds: keepaliveSeconds ?? this.keepaliveSeconds,
      revision: revision,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'roaming_allowed': roamingAllowed,
      'remote_wake_enabled': remoteWakeEnabled,
      'metered_upload_limit': meteredUploadLimit,
      'preference': radioPreferenceToWire(preference),
      'monthly_quota_mb': monthlyQuotaMb,
      'low_quota_warn_percent': lowQuotaWarnPercent,
      'keepalive_seconds': keepaliveSeconds,
    };
  }

  factory CellularPolicy.fromJson(Map<String, Object?> json) {
    return CellularPolicy(
      enabled: json['enabled'] == true,
      roamingAllowed: json['roaming_allowed'] == true,
      remoteWakeEnabled: json['remote_wake_enabled'] == true,
      meteredUploadLimit: json['metered_upload_limit'] == true,
      preference: radioPreferenceFromWire(
        json['preference']?.toString() ?? 'wifi_first',
      ),
      monthlyQuotaMb: _asInt(json['monthly_quota_mb']),
      lowQuotaWarnPercent: _asInt(json['low_quota_warn_percent']),
      keepaliveSeconds: _asInt(json['keepalive_seconds']),
      revision: _asInt(json['revision']),
    );
  }
}

/// The cellular capability of one device as last reported.
class CellularCapability {
  const CellularCapability({
    required this.supported,
    required this.roaming,
    required this.esimState,
    required this.signalPercent,
    required this.quotaMb,
    required this.usedMb,
    required this.quotaExhausted,
    required this.policyRevision,
    required this.appliedRevision,
    required this.upToDate,
  });

  final bool supported;
  final bool roaming;
  final String esimState;
  final int signalPercent;
  final int quotaMb;
  final int usedMb;
  final bool quotaExhausted;
  final int policyRevision;
  final int appliedRevision;
  final bool upToDate;

  factory CellularCapability.fromJson(Map<String, Object?> json) {
    return CellularCapability(
      supported: json['supported'] == true,
      roaming: json['roaming'] == true,
      esimState: json['esim_state']?.toString() ?? 'absent',
      signalPercent: _asInt(json['signal_percent']),
      quotaMb: _asInt(json['quota_mb']),
      usedMb: _asInt(json['used_mb']),
      quotaExhausted: json['quota_exhausted'] == true,
      policyRevision: _asInt(json['policy_revision']),
      appliedRevision: _asInt(json['applied_revision']),
      upToDate: json['up_to_date'] == true,
    );
  }
}

int _asInt(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return 0;
}
