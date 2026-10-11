import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/offline_policy/data/offline_policy_api.dart';
import 'package:parent_app/features/offline_policy/domain/offline_policy.dart';

void main() {
  test('parses the offline policy envelope', () async {
    final api = HttpOfflinePolicyApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'offline_enabled': true,
            'max_cached_packages': 30,
            'max_cache_bytes': 268435456,
            'cache_ttl_hours': 48,
            'download_over_wifi_only': false,
            'revision': 4,
          },
        },
      ),
    );

    final policy = await api.fetchPolicy();

    expect(policy.offlineEnabled, isTrue);
    expect(policy.maxCachedPackages, 30);
    expect(policy.maxCacheMegabytes, 256);
    expect(policy.cacheTtlHours, 48);
    expect(policy.downloadOverWifiOnly, isFalse);
    expect(policy.revision, 4);
  });

  test('publishes the policy with the platform field names', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'offline_enabled': false,
          'max_cached_packages': 10,
          'max_cache_bytes': 16777216,
          'cache_ttl_hours': 6,
          'download_over_wifi_only': true,
          'revision': 1,
        },
      },
    );
    final api = HttpOfflinePolicyApi(client);

    await api.publishPolicy(
      const OfflinePolicy(
        offlineEnabled: false,
        maxCachedPackages: 10,
        maxCacheBytes: 16777216,
        cacheTtlHours: 6,
        downloadOverWifiOnly: true,
        revision: 0,
      ),
    );

    expect(client.putPath, '/api/v1/content/offline-policy');
    final body = client.lastPutBody! as Map<String, Object?>;
    expect(body['max_cached_packages'], 10);
    expect(body['max_cache_bytes'], 16777216);
    expect(body['offline_enabled'], isFalse);
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  String? putPath;
  Object? lastPutBody;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async => response;

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    putPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
