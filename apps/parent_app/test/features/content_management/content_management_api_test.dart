import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  test('parses the download envelope returned by the platform', () async {
    final api = HttpContentManagementApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'download': <String, Object?>{
              'download_url': 'https://downloads.example.com/story.bin',
              'sha256': 'a' * 64,
              'size_bytes': 4096,
            },
          },
        },
      ),
    );

    final spec = await api.fetchDownloadSpec('story_001');

    expect(spec.downloadUrl, 'https://downloads.example.com/story.bin');
    expect(spec.sha256, 'a' * 64);
    expect(spec.sizeBytes, 4096);
  });

  test('sends backend age-tier values when filtering the catalog', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'catalog_revision': 1,
          'packages': <Object?>[],
          'withdrawn_package_ids': <Object?>[],
        },
      },
    );
    final api = HttpContentManagementApi(client);

    await api.fetchCatalog(sinceRevision: 0, ageTier: ContentAgeTier.age3to4);

    expect(client.lastQueryParameters, <String, String>{
      'since_revision': '0',
      'age_tier': 'age_3_4',
    });
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  Map<String, String>? lastQueryParameters;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastQueryParameters = queryParameters;
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

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) {
    throw UnimplementedError();
  }
}
