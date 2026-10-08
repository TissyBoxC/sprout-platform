/// Network provisioning state reported by a bound device.
///
/// Older platform responses may omit this object entirely, so callers should
/// treat a null status as unknown rather than as a failed device.
class DeviceProvisioningStatus {
  const DeviceProvisioningStatus({
    required this.state,
    required this.wifiConfigured,
    required this.sessionState,
    required this.droppedEvents,
    required this.lastProvisionedAt,
  });

  final String state;
  final bool wifiConfigured;
  final String sessionState;
  final int droppedEvents;
  final DateTime? lastProvisionedAt;

  factory DeviceProvisioningStatus.fromJson(Map<String, Object?> json) {
    return DeviceProvisioningStatus(
      state: _asString(json['state']),
      wifiConfigured: json['wifi_configured'] == true,
      sessionState: _asString(json['session_state']),
      droppedEvents: _asNonNegativeInt(json['dropped_events']),
      lastProvisionedAt: _asDateTime(json['last_provisioned_at']),
    );
  }
}

String _asString(Object? value) {
  return value is String ? value : '';
}

DateTime? _asDateTime(Object? value) {
  if (value is! String || value.isEmpty) {
    return null;
  }
  return DateTime.tryParse(value);
}

int _asNonNegativeInt(Object? value) {
  final parsed = value is num ? value.toInt() : int.tryParse('${value ?? ''}');
  return parsed == null || parsed < 0 ? 0 : parsed;
}
