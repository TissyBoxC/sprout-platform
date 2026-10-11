import '../../../core/network/api_client.dart';
import '../domain/offline_policy.dart';

/// Guardian-facing offline download policy contract.
///
/// The policy is platform-wide, so the client only ever reads or replaces the
/// single published revision and never sends an owner id.
abstract interface class OfflinePolicyApi {
  Future<OfflinePolicy> fetchPolicy();

  Future<OfflinePolicy> publishPolicy(OfflinePolicy policy);
}

/// Production implementation backed by the authenticated API client.
class HttpOfflinePolicyApi implements OfflinePolicyApi {
  const HttpOfflinePolicyApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<OfflinePolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/content/offline-policy');
    return OfflinePolicy.fromJson(_data(response));
  }

  @override
  Future<OfflinePolicy> publishPolicy(OfflinePolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/content/offline-policy',
      body: <String, Object?>{
        'offline_enabled': policy.offlineEnabled,
        'max_cached_packages': policy.maxCachedPackages,
        'max_cache_bytes': policy.maxCacheBytes,
        'cache_ttl_hours': policy.cacheTtlHours,
        'download_over_wifi_only': policy.downloadOverWifiOnly,
      },
    );
    return OfflinePolicy.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing offline policy data');
  }
  return Map<String, Object?>.from(data);
}
