import '../../../core/network/api_client.dart';
import '../domain/motion_policy.dart';

/// Guardian-facing motion and motor contract.
abstract interface class MotionApi {
  Future<MotionPolicy> fetchPolicy();

  Future<MotionPolicy> updatePolicy(MotionPolicy policy);

  Future<MotionCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpMotionApi implements MotionApi {
  const HttpMotionApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<MotionPolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/motion/policy');
    return MotionPolicy.fromJson(_data(response));
  }

  @override
  Future<MotionPolicy> updatePolicy(MotionPolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/motion/policy',
      body: policy.toJson(),
    );
    return MotionPolicy.fromJson(_data(response));
  }

  @override
  Future<MotionCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/motion/capability',
    );
    return MotionCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing motion data');
  }
  return Map<String, Object?>.from(data);
}
