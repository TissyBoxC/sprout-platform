/// Guardian-facing wearable location, geofence and SOS contract types.
///
/// The platform stores only the bounded metadata it needs: a rounded position,
/// the precision band, the geofence result and the SOS lifecycle. Revoking the
/// location permission stops sampling, upload and display and clears the
/// retained positions.
library;

/// One family location privacy policy.
class LocationPolicy {
  const LocationPolicy({
    required this.enabled,
    required this.permissionGranted,
    required this.precision,
    required this.uploadIntervalSeconds,
    required this.retentionDays,
    required this.geofenceRadiusMeters,
  });

  final bool enabled;
  final bool permissionGranted;
  final String precision;
  final int uploadIntervalSeconds;
  final int retentionDays;
  final int geofenceRadiusMeters;

  LocationPolicy copyWith({
    bool? enabled,
    bool? permissionGranted,
    String? precision,
    int? uploadIntervalSeconds,
    int? retentionDays,
    int? geofenceRadiusMeters,
  }) {
    return LocationPolicy(
      enabled: enabled ?? this.enabled,
      permissionGranted: permissionGranted ?? this.permissionGranted,
      precision: precision ?? this.precision,
      uploadIntervalSeconds: uploadIntervalSeconds ?? this.uploadIntervalSeconds,
      retentionDays: retentionDays ?? this.retentionDays,
      geofenceRadiusMeters: geofenceRadiusMeters ?? this.geofenceRadiusMeters,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'permission_granted': permissionGranted,
      'precision': precision,
      'upload_interval_seconds': uploadIntervalSeconds,
      'retention_days': retentionDays,
      'geofence_radius_meters': geofenceRadiusMeters,
    };
  }

  factory LocationPolicy.fromJson(Map<String, Object?> json) {
    return LocationPolicy(
      enabled: json['enabled'] == true,
      permissionGranted: json['permission_granted'] == true,
      precision: json['precision']?.toString() ?? 'coarse',
      uploadIntervalSeconds: _intValue(json['upload_interval_seconds'], 900),
      retentionDays: _intValue(json['retention_days'], 7),
      geofenceRadiusMeters: _intValue(json['geofence_radius_meters'], 500),
    );
  }

  static const LocationPolicy fallback = LocationPolicy(
    enabled: false,
    permissionGranted: false,
    precision: 'coarse',
    uploadIntervalSeconds: 900,
    retentionDays: 7,
    geofenceRadiusMeters: 500,
  );
}

/// One guardian-managed emergency contact.
class EmergencyContact {
  const EmergencyContact({
    required this.id,
    required this.childId,
    required this.displayName,
    required this.phone,
    required this.role,
    required this.priority,
    required this.enabled,
  });

  final String id;
  final String childId;
  final String displayName;
  final String phone;
  final String role;
  final int priority;
  final bool enabled;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'child_id': childId,
      'display_name': displayName,
      'phone': phone,
      'role': role,
      'priority': priority,
      'enabled': enabled,
    };
  }

  factory EmergencyContact.fromJson(Map<String, Object?> json) {
    return EmergencyContact(
      id: json['id']?.toString() ?? '',
      childId: json['child_id']?.toString() ?? '',
      displayName: json['display_name']?.toString() ?? '',
      phone: json['phone']?.toString() ?? '',
      role: json['role']?.toString() ?? 'guardian',
      priority: _intValue(json['priority'], 0),
      enabled: json['enabled'] == true,
    );
  }
}

/// The single family home geofence.
class HomeGeofence {
  const HomeGeofence({
    required this.id,
    required this.childId,
    required this.latitude,
    required this.longitude,
    required this.radiusMeters,
    required this.enabled,
  });

  final String id;
  final String childId;
  final double latitude;
  final double longitude;
  final int radiusMeters;
  final bool enabled;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'child_id': childId,
      'latitude': latitude,
      'longitude': longitude,
      'radius_meters': radiusMeters,
      'enabled': enabled,
    };
  }

  factory HomeGeofence.fromJson(Map<String, Object?> json) {
    return HomeGeofence(
      id: json['id']?.toString() ?? '',
      childId: json['child_id']?.toString() ?? '',
      latitude: _doubleValue(json['latitude'], 0),
      longitude: _doubleValue(json['longitude'], 0),
      radiusMeters: _intValue(json['radius_meters'], 500),
      enabled: json['enabled'] == true,
    );
  }
}

/// One device wearable snapshot.
class WearableDeviceStatus {
  const WearableDeviceStatus({
    required this.deviceId,
    required this.locationEnabled,
    required this.permissionGranted,
    required this.precision,
    required this.sosState,
    required this.latitude,
    required this.longitude,
    required this.accuracyMeters,
    required this.geofenceState,
  });

  final String deviceId;
  final bool locationEnabled;
  final bool permissionGranted;
  final String precision;
  final String sosState;
  final double latitude;
  final double longitude;
  final double accuracyMeters;
  final String geofenceState;

  bool get isSOSActive => sosState == 'triggered';

  factory WearableDeviceStatus.fromJson(Map<String, Object?> json) {
    return WearableDeviceStatus(
      deviceId: json['device_id']?.toString() ?? '',
      locationEnabled: json['location_enabled'] == true,
      permissionGranted: json['permission_granted'] == true,
      precision: json['precision']?.toString() ?? 'coarse',
      sosState: json['sos_state']?.toString() ?? 'idle',
      latitude: _doubleValue(json['latitude'], 0),
      longitude: _doubleValue(json['longitude'], 0),
      accuracyMeters: _doubleValue(json['accuracy_meters'], 0),
      geofenceState: json['geofence_state']?.toString() ?? 'unknown',
    );
  }
}

/// One SOS event.
class SosEvent {
  const SosEvent({
    required this.id,
    required this.deviceId,
    required this.latitude,
    required this.longitude,
    required this.state,
  });

  final String id;
  final String deviceId;
  final double latitude;
  final double longitude;
  final String state;

  bool get isActive => state == 'triggered';

  factory SosEvent.fromJson(Map<String, Object?> json) {
    return SosEvent(
      id: json['id']?.toString() ?? '',
      deviceId: json['device_id']?.toString() ?? '',
      latitude: _doubleValue(json['latitude'], 0),
      longitude: _doubleValue(json['longitude'], 0),
      state: json['state']?.toString() ?? 'triggered',
    );
  }
}

int _intValue(Object? value, int fallback) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? fallback;
}

double _doubleValue(Object? value, double fallback) {
  if (value is num) {
    return value.toDouble();
  }
  return double.tryParse(value?.toString() ?? '') ?? fallback;
}
