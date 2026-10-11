import '../../../core/network/api_client.dart';
import '../domain/cellular_policy.dart';

/// Guardian-facing cellular policy contract.
abstract interface class CellularApi {
  Future<CellularPolicy> fetchPolicy();

  Future<CellularPolicy> updatePolicy(CellularPolicy policy);

  Future<CellularCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpCellularApi implements CellularApi {
  const HttpCellularApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<CellularPolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/connectivity/policy');
    return CellularPolicy.fromJson(_data(response));
  }

  @override
  Future<CellularPolicy> updatePolicy(CellularPolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/connectivity/policy',
      body: policy.toJson(),
    );
    return CellularPolicy.fromJson(_data(response));
  }

  @override
  Future<CellularCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/connectivity/capability',
    );
    return CellularCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing cellular data');
  }
  return Map<String, Object?>.from(data);
}
