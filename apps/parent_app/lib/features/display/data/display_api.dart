import '../../../core/network/api_client.dart';
import '../domain/display_policy.dart';

/// Guardian-facing eye-care display contract.
abstract interface class DisplayApi {
  Future<DisplayPolicy> fetchPolicy();

  Future<DisplayPolicy> updatePolicy(DisplayPolicy policy);

  Future<DisplayCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpDisplayApi implements DisplayApi {
  const HttpDisplayApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<DisplayPolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/display/policy');
    return DisplayPolicy.fromJson(_data(response));
  }

  @override
  Future<DisplayPolicy> updatePolicy(DisplayPolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/display/policy',
      body: policy.toJson(),
    );
    return DisplayPolicy.fromJson(_data(response));
  }

  @override
  Future<DisplayCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/display/capability',
    );
    return DisplayCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing display data');
  }
  return Map<String, Object?>.from(data);
}
