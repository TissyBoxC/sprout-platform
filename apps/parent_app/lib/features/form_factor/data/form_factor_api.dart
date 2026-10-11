import '../../../core/network/api_client.dart';
import '../domain/form_factor_profile.dart';

/// Guardian-facing form-factor contract.
abstract interface class FormFactorApi {
  Future<FormFactorCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpFormFactorApi implements FormFactorApi {
  const HttpFormFactorApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<FormFactorCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/form-factor/capability',
    );
    return FormFactorCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing form factor data');
  }
  return Map<String, Object?>.from(data);
}
