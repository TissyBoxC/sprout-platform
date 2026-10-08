import '../../../core/network/api_client.dart';
import '../domain/privacy_state.dart';

/// HTTP boundary for guardian consent, export, and account deletion.
class PrivacyApi {
  const PrivacyApi(this._apiClient);

  final ApiClient _apiClient;

  Future<PrivacyStatus> status() async {
    final response = await _apiClient.get('/api/v1/privacy');
    return PrivacyStatus.fromJson(_payload(response));
  }

  Future<Map<String, Object?>> export() async {
    final response = await _apiClient.get('/api/v1/privacy/export');
    return _payload(response);
  }

  Future<PrivacyDeletion> requestDeletion() async {
    final response = await _apiClient.post(
      '/api/v1/privacy/deletion',
      body: const {'confirmation': 'DELETE'},
    );
    return _deletionFrom(response);
  }

  Future<PrivacyDeletion> cancelDeletion() async {
    final response = await _apiClient.delete('/api/v1/privacy/deletion');
    return _deletionFrom(response);
  }

  Future<GuardianConsent> withdrawConsent() async {
    final response = await _apiClient.post(
      '/api/v1/privacy/consent/withdraw',
      body: const {'confirmation': 'WITHDRAW'},
    );
    final payload = _payload(response);
    final consent = payload['consent'];
    return GuardianConsent.fromJson(
      consent is Map
          ? Map<String, Object?>.from(consent)
          : _requiredMap(payload, 'consent'),
    );
  }

  PrivacyDeletion _deletionFrom(Map<String, Object?> response) {
    final payload = _payload(response);
    final deletion = payload['deletion'];
    if (deletion is Map) {
      return PrivacyDeletion.fromJson(Map<String, Object?>.from(deletion));
    }
    return PrivacyDeletion.fromJson(payload);
  }
}

Map<String, Object?> _payload(Map<String, Object?> response) {
  final data = response['data'];
  if (data is Map) {
    return Map<String, Object?>.from(data);
  }
  return response;
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}
