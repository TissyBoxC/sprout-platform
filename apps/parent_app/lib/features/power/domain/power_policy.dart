/// Guardian-facing battery and low-power policy contract types.
///
/// The platform stores only bounded policy scalars and bounded device power
/// status; raw gauge telemetry never flows through this client.
library;

/// One validated battery and low-power policy for the family.
class PowerPolicy {
  const PowerPolicy({
    required this.enabled,
    required this.lowPowerEnabled,
    required this.deepSleepEnabled,
    required this.chargePriority,
    required this.lowBatteryWarnPercent,
    required this.lowPowerEnterPercent,
    required this.sleepAfterSeconds,
    required this.revision,
  });

  final bool enabled;
  final bool lowPowerEnabled;
  final bool deepSleepEnabled;
  final bool chargePriority;
  final int lowBatteryWarnPercent;
  final int lowPowerEnterPercent;
  final int sleepAfterSeconds;
  final int revision;

  PowerPolicy copyWith({
    bool? enabled,
    bool? lowPowerEnabled,
    bool? deepSleepEnabled,
    bool? chargePriority,
    int? lowBatteryWarnPercent,
    int? lowPowerEnterPercent,
    int? sleepAfterSeconds,
  }) {
    return PowerPolicy(
      enabled: enabled ?? this.enabled,
      lowPowerEnabled: lowPowerEnabled ?? this.lowPowerEnabled,
      deepSleepEnabled: deepSleepEnabled ?? this.deepSleepEnabled,
      chargePriority: chargePriority ?? this.chargePriority,
      lowBatteryWarnPercent:
          lowBatteryWarnPercent ?? this.lowBatteryWarnPercent,
      lowPowerEnterPercent: lowPowerEnterPercent ?? this.lowPowerEnterPercent,
      sleepAfterSeconds: sleepAfterSeconds ?? this.sleepAfterSeconds,
      revision: revision,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'low_power_enabled': lowPowerEnabled,
      'deep_sleep_enabled': deepSleepEnabled,
      'charge_priority': chargePriority,
      'low_battery_warn_percent': lowBatteryWarnPercent,
      'low_power_enter_percent': lowPowerEnterPercent,
      'sleep_after_seconds': sleepAfterSeconds,
    };
  }

  factory PowerPolicy.fromJson(Map<String, Object?> json) {
    return PowerPolicy(
      enabled: json['enabled'] == true,
      lowPowerEnabled: json['low_power_enabled'] == true,
      deepSleepEnabled: json['deep_sleep_enabled'] == true,
      chargePriority: json['charge_priority'] == true,
      lowBatteryWarnPercent: _asInt(json['low_battery_warn_percent']),
      lowPowerEnterPercent: _asInt(json['low_power_enter_percent']),
      sleepAfterSeconds: _asInt(json['sleep_after_seconds']),
      revision: _asInt(json['revision']),
    );
  }
}

/// The battery capability of one device as last reported.
class PowerCapability {
  const PowerCapability({
    required this.supported,
    required this.chargeState,
    required this.powerMode,
    required this.batteryPercent,
    required this.charging,
    required this.lowWarning,
    required this.policyRevision,
    required this.appliedRevision,
    required this.upToDate,
  });

  final bool supported;
  final String chargeState;
  final String powerMode;
  final int batteryPercent;
  final bool charging;
  final bool lowWarning;
  final int policyRevision;
  final int appliedRevision;
  final bool upToDate;

  factory PowerCapability.fromJson(Map<String, Object?> json) {
    return PowerCapability(
      supported: json['supported'] == true,
      chargeState: json['charge_state']?.toString() ?? 'unknown',
      powerMode: json['power_mode']?.toString() ?? 'normal',
      batteryPercent: _asInt(json['battery_percent']),
      charging: json['charging'] == true,
      lowWarning: json['low_warning'] == true,
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
