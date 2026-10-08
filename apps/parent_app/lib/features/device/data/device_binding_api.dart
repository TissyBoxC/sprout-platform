import '../../../core/network/api_client.dart';
import '../domain/device_provisioning_status.dart';

/// Device information returned by the platform binding API.
class BoundDevice {
  const BoundDevice({
    required this.deviceId,
    required this.deviceName,
    required this.hardwareModel,
    required this.firmwareVersion,
    required this.capabilities,
    required this.lifecycleStatus,
    required this.boundAt,
    required this.runtime,
  });

  final String deviceId;
  final String deviceName;
  final String hardwareModel;
  final String firmwareVersion;
  final List<String> capabilities;
  final String lifecycleStatus;
  final DateTime boundAt;
  final DeviceRuntimeStatus? runtime;

  factory BoundDevice.fromJson(Map<String, Object?> json) {
    final boundAtValue = json['bound_at'];
    final runtimeValue = json['runtime'];
    return BoundDevice(
      deviceId: _requiredString(json['device_id'], 'device_id'),
      deviceName: _requiredString(json['device_name'], 'device_name'),
      hardwareModel: _asString(json['hardware_model']),
      firmwareVersion: _asString(json['firmware_version']),
      capabilities: _stringList(json['capabilities']),
      lifecycleStatus: _asString(json['lifecycle_status']),
      boundAt: boundAtValue is String
          ? DateTime.tryParse(boundAtValue) ?? DateTime.now()
          : DateTime.now(),
      runtime: runtimeValue is Map
          ? DeviceRuntimeStatus.fromJson(
              Map<String, Object?>.from(runtimeValue),
            )
          : null,
    );
  }
}

/// Last known operational state reported by a device.
class DeviceRuntimeStatus {
  const DeviceRuntimeStatus({
    required this.isOnline,
    required this.connectionState,
    required this.transport,
    required this.networkQuality,
    required this.rssiDbm,
    required this.latencyMs,
    required this.packetLossPercent,
    required this.timeSyncState,
    required this.lastSyncedAt,
    required this.offlineState,
    required this.offlineReason,
    required this.fallbackActive,
    required this.pendingTelemetry,
    required this.reportedAt,
    required this.receivedAt,
    required this.provisioning,
  });

  final bool isOnline;
  final String connectionState;
  final String transport;
  final String networkQuality;
  final int rssiDbm;
  final int latencyMs;
  final int packetLossPercent;
  final String timeSyncState;
  final DateTime? lastSyncedAt;
  final String offlineState;
  final String offlineReason;
  final bool fallbackActive;
  final int pendingTelemetry;
  final DateTime? reportedAt;
  final DateTime? receivedAt;
  final DeviceProvisioningStatus? provisioning;

  factory DeviceRuntimeStatus.fromJson(Map<String, Object?> json) {
    final connection = _map(json['connection']);
    final quality = _map(json['network_quality']);
    final timeSync = _map(json['time_sync']);
    final offline = _map(json['offline']);
    final provisioning = json['provisioning'];
    return DeviceRuntimeStatus(
      isOnline: json['is_online'] == true,
      connectionState: _asString(connection['state']),
      transport: _asString(connection['transport']),
      networkQuality: _asString(quality['level']),
      rssiDbm: _asInt(quality['rssi_dbm']),
      latencyMs: _asInt(quality['latency_ms']),
      packetLossPercent: _asInt(quality['packet_loss_percent']),
      timeSyncState: _asString(timeSync['state']),
      lastSyncedAt: _asDateTime(timeSync['last_synced_at']),
      offlineState: _asString(offline['state']),
      offlineReason: _asString(offline['reason']),
      fallbackActive: offline['fallback_active'] == true,
      pendingTelemetry: _asInt(offline['pending_telemetry']),
      reportedAt: _asDateTime(json['reported_at']),
      receivedAt: _asDateTime(json['received_at']),
      provisioning: provisioning is Map
          ? DeviceProvisioningStatus.fromJson(
              Map<String, Object?>.from(provisioning),
            )
          : null,
    );
  }
}

/// Binding API used by the scan and BLE provisioning flows.
class DeviceBindingApi {
  const DeviceBindingApi(this._apiClient);

  final ApiClient _apiClient;

  Future<List<BoundDevice>> list() async {
    final response = await _apiClient.get('/api/v1/devices/status');
    final data = response['data'] as Map<String, Object?>;
    final devices = data['devices'];
    if (devices is! List) {
      return const [];
    }
    return devices
        .whereType<Map>()
        .map((item) => BoundDevice.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  /// Requests a one-time binding code from a device that has authenticated
  /// over its local BLE or Wi-Fi provisioning channel.
  Future<DeviceProvisioningTicket> requestProvisioningTicket(
    String deviceId,
    String deviceSessionToken,
  ) async {
    final response = await _apiClient.postWithBearerToken(
      '/api/v1/devices/$deviceId/provisioning-token',
      bearerToken: deviceSessionToken,
    );
    return DeviceProvisioningTicket.fromJson(
      response['data'] as Map<String, Object?>,
    );
  }

  Future<BoundDevice> bind({
    required String token,
    required String deviceName,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/devices/bind',
      body: {'token': token, 'device_name': deviceName},
    );
    return BoundDevice.fromJson(response['data'] as Map<String, Object?>);
  }

  Future<void> remove(String deviceId) async {
    await _apiClient.delete('/api/v1/devices/$deviceId');
  }
}

/// One-time code and expiry issued by an authenticated device.
class DeviceProvisioningTicket {
  const DeviceProvisioningTicket({
    required this.token,
    required this.deviceId,
    required this.expiresAt,
  });

  final String token;
  final String deviceId;
  final DateTime expiresAt;

  factory DeviceProvisioningTicket.fromJson(Map<String, Object?> json) {
    final expiresAtValue = json['expires_at'];
    return DeviceProvisioningTicket(
      token: _requiredString(json['token'], 'token'),
      deviceId: _requiredString(json['device_id'], 'device_id'),
      expiresAt: expiresAtValue is String
          ? DateTime.tryParse(expiresAtValue) ?? DateTime.now()
          : DateTime.now(),
    );
  }
}

String _requiredString(Object? value, String field) {
  if (value is String && value.isNotEmpty) {
    return value;
  }
  throw FormatException('missing $field');
}

String _asString(Object? value) {
  return value is String ? value : '';
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value.whereType<String>().toList(growable: false);
}

Map<String, Object?> _map(Object? value) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  return const {};
}

int _asInt(Object? value) {
  return value is num ? value.toInt() : 0;
}

DateTime? _asDateTime(Object? value) {
  if (value is! String || value.isEmpty) {
    return null;
  }
  return DateTime.tryParse(value);
}
