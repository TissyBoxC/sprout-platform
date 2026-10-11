/// Guardian-facing eye-care display contract types.
///
/// The platform stores only bounded scalar policy values and bounded device
/// screen status; no screenshots or pixels ever flow through this client.
library;

/// One validated eye-care policy for the family.
class DisplayPolicy {
  const DisplayPolicy({
    required this.brightnessPercent,
    required this.colorTemperatureKelvin,
    required this.minViewDistanceCm,
    required this.dailyLimitMinutes,
    required this.breakIntervalMinutes,
    required this.dimAfterSeconds,
    required this.autoBrightness,
    required this.nightLight,
    required this.distanceReminder,
    required this.revision,
  });

  final int brightnessPercent;
  final int colorTemperatureKelvin;
  final int minViewDistanceCm;
  final int dailyLimitMinutes;
  final int breakIntervalMinutes;
  final int dimAfterSeconds;
  final bool autoBrightness;
  final bool nightLight;
  final bool distanceReminder;
  final int revision;

  DisplayPolicy copyWith({
    int? brightnessPercent,
    int? colorTemperatureKelvin,
    int? minViewDistanceCm,
    int? dailyLimitMinutes,
    int? breakIntervalMinutes,
    int? dimAfterSeconds,
    bool? autoBrightness,
    bool? nightLight,
    bool? distanceReminder,
  }) {
    return DisplayPolicy(
      brightnessPercent: brightnessPercent ?? this.brightnessPercent,
      colorTemperatureKelvin:
          colorTemperatureKelvin ?? this.colorTemperatureKelvin,
      minViewDistanceCm: minViewDistanceCm ?? this.minViewDistanceCm,
      dailyLimitMinutes: dailyLimitMinutes ?? this.dailyLimitMinutes,
      breakIntervalMinutes: breakIntervalMinutes ?? this.breakIntervalMinutes,
      dimAfterSeconds: dimAfterSeconds ?? this.dimAfterSeconds,
      autoBrightness: autoBrightness ?? this.autoBrightness,
      nightLight: nightLight ?? this.nightLight,
      distanceReminder: distanceReminder ?? this.distanceReminder,
      revision: revision,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'brightness_percent': brightnessPercent,
      'color_temperature_kelvin': colorTemperatureKelvin,
      'min_view_distance_cm': minViewDistanceCm,
      'daily_limit_minutes': dailyLimitMinutes,
      'break_interval_minutes': breakIntervalMinutes,
      'dim_after_seconds': dimAfterSeconds,
      'auto_brightness': autoBrightness,
      'night_light': nightLight,
      'distance_reminder': distanceReminder,
    };
  }

  factory DisplayPolicy.fromJson(Map<String, Object?> json) {
    return DisplayPolicy(
      brightnessPercent: _asInt(json['brightness_percent']),
      colorTemperatureKelvin: _asInt(json['color_temperature_kelvin']),
      minViewDistanceCm: _asInt(json['min_view_distance_cm']),
      dailyLimitMinutes: _asInt(json['daily_limit_minutes']),
      breakIntervalMinutes: _asInt(json['break_interval_minutes']),
      dimAfterSeconds: _asInt(json['dim_after_seconds']),
      autoBrightness: json['auto_brightness'] == true,
      nightLight: json['night_light'] == true,
      distanceReminder: json['distance_reminder'] == true,
      revision: _asInt(json['revision']),
    );
  }
}

/// The display capability of one device as last reported.
class DisplayCapability {
  const DisplayCapability({
    required this.supported,
    required this.touchSupported,
    required this.screenWidth,
    required this.screenHeight,
    required this.policyRevision,
    required this.appliedRevision,
    required this.upToDate,
  });

  final bool supported;
  final bool touchSupported;
  final int screenWidth;
  final int screenHeight;
  final int policyRevision;
  final int appliedRevision;
  final bool upToDate;

  factory DisplayCapability.fromJson(Map<String, Object?> json) {
    return DisplayCapability(
      supported: json['supported'] == true,
      touchSupported: json['touch_supported'] == true,
      screenWidth: _asInt(json['screen_width']),
      screenHeight: _asInt(json['screen_height']),
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
