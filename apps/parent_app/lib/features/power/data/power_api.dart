import '../../../core/network/api_client.dart';
import '../domain/power_policy.dart';

/// Guardian-facing battery and low-power contract.
abstract interface class PowerApi {
  Future<PowerPolicy> fetchPolicy();

  Future<PowerPolicy> updatePolicy(PowerPolicy policy);

  Future<PowerCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpPowerApi implements PowerApi {
  const HttpPowerApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<PowerPolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/power/policy');
    return PowerPolicy.fromJson(_data(response));
  }

  @override
  Future<PowerPolicy> updatePolicy(PowerPolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/power/policy',
      body: policy.toJson(),
    );
    return PowerPolicy.fromJson(_data(response));
  }

  @override
  Future<PowerCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/power/capability',
    );
    return PowerCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing power data');
  }
  return Map<String, Object?>.from(data);
}
