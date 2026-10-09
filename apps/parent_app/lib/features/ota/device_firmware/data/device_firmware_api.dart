import '../../../../core/network/api_client.dart';
import '../domain/device_firmware_update.dart';

/// Parent-authenticated HTTP boundary for one device's firmware update.
class DeviceFirmwareApi {
  const DeviceFirmwareApi(this._apiClient);

  final ApiClient _apiClient;

  Future<DeviceFirmwareUpdate> fetch(String deviceId) async {
    final response = await _apiClient.get(_basePath(deviceId));
    return _parseUpdate(response);
  }

  Future<DeviceFirmwareUpdate> install(String deviceId) async {
    final response = await _apiClient.post(_installPath(deviceId));
    return _parseUpdate(response);
  }

  Future<DeviceFirmwareUpdate> status(String deviceId) async {
    final response = await _apiClient.get(_statusPath(deviceId));
    return _parseUpdate(response);
  }

  Future<DeviceFirmwareUpdate> retry(String deviceId) async {
    final response = await _apiClient.post(_retryPath(deviceId));
    return _parseUpdate(response);
  }

  Future<DeviceFirmwareUpdate> rollback(String deviceId) async {
    final response = await _apiClient.post(_rollbackPath(deviceId));
    return _parseUpdate(response);
  }

  String _basePath(String deviceId) => '/api/v1/devices/$deviceId/ota';
  String _installPath(String deviceId) => '${_basePath(deviceId)}/install';
  String _statusPath(String deviceId) => '${_basePath(deviceId)}/status';
  String _retryPath(String deviceId) => '${_basePath(deviceId)}/retry';
  String _rollbackPath(String deviceId) => '${_basePath(deviceId)}/rollback';

  DeviceFirmwareUpdate _parseUpdate(Map<String, Object?> response) {
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing data');
    }
    return DeviceFirmwareUpdate.fromJson(Map<String, Object?>.from(data));
  }
}
