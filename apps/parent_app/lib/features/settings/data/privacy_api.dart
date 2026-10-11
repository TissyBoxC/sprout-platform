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

  /// Withdraws one consent type. Returns the updated status so per-type
  /// consents stay in sync; callers that only need the summary can read
  /// [PrivacyStatus.consent].
  Future<PrivacyStatus> withdrawConsent({
    GuardianConsentKind consentType =
        GuardianConsentKind.childDataProcessing,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/privacy/consent/withdraw',
      body: {'confirmation': 'WITHDRAW', 'consent_type': consentType.wireValue},
    );
    return _statusFrom(response);
  }

  /// Restores one consent type after a withdrawal.
  Future<PrivacyStatus> grantConsent({
    GuardianConsentKind consentType =
        GuardianConsentKind.childDataProcessing,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/privacy/consent/grant',
      body: {'consent_type': consentType.wireValue},
    );
    return _statusFrom(response);
  }

  PrivacyStatus _statusFrom(Map<String, Object?> response) {
    final payload = _payload(response);
    // The platform answers with the whole privacy status. An older server may
    // reply with only the consent summary; synthesize the fields the parser
    // needs and let the controller merge just the consent projection.
    final isFullStatus =
        payload['deletion'] is Map ||
        payload['policy'] is Map ||
        payload['consents'] is List;
    if (isFullStatus) {
      return PrivacyStatus.fromJson(payload);
    }
    final consent = payload['consent'] is Map
        ? Map<String, Object?>.from(payload['consent'] as Map)
        : _requiredMap(payload, 'consent');
    return PrivacyStatus.fromJson({
      'consent': consent,
      'deletion': const <String, Object?>{'status': 'none'},
      'policy': const <String, Object?>{
        'audio_retention_days': 0,
        'image_retention_days': 0,
        'conversation_retention_days': 0,
      },
    });
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
