import '../../../core/network/api_client.dart';
import '../domain/parent_policy.dart';

/// HTTP data source for the time and content policy of one child.
class ParentPolicyApi {
  const ParentPolicyApi(this._apiClient);

  final ApiClient _apiClient;

  Future<ParentPolicy> get(String childId) async {
    final response = await _apiClient.get('/api/v1/children/$childId/policy');
    final data = _requiredMap(response['data'], 'data');
    return ParentPolicy.fromJson(_requiredMap(data['policy'], 'policy'));
  }

  Future<ParentPolicy> update(String childId, ParentPolicyDraft draft) async {
    final response = await _apiClient.put(
      '/api/v1/children/$childId/policy',
      body: draft.toJson(),
    );
    final data = _requiredMap(response['data'], 'data');
    return ParentPolicy.fromJson(_requiredMap(data['policy'], 'policy'));
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}
