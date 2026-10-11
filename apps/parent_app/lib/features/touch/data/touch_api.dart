import '../../../core/network/api_client.dart';
import '../domain/touch_config.dart';

/// Guardian-facing touch input contract.
abstract interface class TouchApi {
  Future<TouchConfig> fetchConfig();

  Future<TouchConfig> updateConfig(TouchConfig config);

  Future<TouchCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpTouchApi implements TouchApi {
  const HttpTouchApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<TouchConfig> fetchConfig() async {
    final response = await _apiClient.get('/api/v1/touch/config');
    return TouchConfig.fromJson(_data(response));
  }

  @override
  Future<TouchConfig> updateConfig(TouchConfig config) async {
    final response = await _apiClient.put(
      '/api/v1/touch/config',
      body: config.toJson(),
    );
    return TouchConfig.fromJson(_data(response));
  }

  @override
  Future<TouchCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/touch/capability',
    );
    return TouchCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing touch data');
  }
  return Map<String, Object?>.from(data);
}
