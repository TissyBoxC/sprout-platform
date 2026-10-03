import '../../../core/network/api_client.dart';
import '../domain/child_profile.dart';

/// HTTP data source for guardian-owned child profiles.
class ChildProfileApi {
  const ChildProfileApi(this._apiClient);

  final ApiClient _apiClient;

  Future<List<ChildProfile>> list() async {
    final response = await _apiClient.get('/api/v1/children');
    final data = _requiredMap(response['data'], 'data');
    final children = data['children'];
    if (children is! List) {
      return const [];
    }
    return children
        .whereType<Map>()
        .map((item) => ChildProfile.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  Future<ChildProfile> create(ChildProfileDraft draft) async {
    final response = await _apiClient.post(
      '/api/v1/children',
      body: draft.toCreateJson(),
    );
    final data = _requiredMap(response['data'], 'data');
    return ChildProfile.fromJson(_requiredMap(data['child'], 'child'));
  }

  Future<ChildProfile> update(String childId, ChildProfileDraft draft) async {
    final response = await _apiClient.put(
      '/api/v1/children/$childId',
      body: draft.toUpdateJson(),
    );
    final data = _requiredMap(response['data'], 'data');
    return ChildProfile.fromJson(_requiredMap(data['child'], 'child'));
  }

  Future<void> delete(String childId) async {
    await _apiClient.delete('/api/v1/children/$childId');
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}
