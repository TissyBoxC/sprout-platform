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

  test('parses the favorites envelope returned by the platform', () async {
    final api = HttpContentManagementApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'favorites': <Object?>[
              <String, Object?>{
                'package_id': 'story_001',
                'title': '会发光的种子',
                'category': 'story',
                'age_tiers': <Object?>['age_3_4'],
                'package_version': 3,
                'available': true,
                'created_at': '2026-10-09T08:30:00Z',
              },
            ],
          },
        },
      ),
    );

    final favorites = await api.fetchFavorites();

    expect(favorites, hasLength(1));
    expect(favorites.single.packageId, 'story_001');
    expect(favorites.single.title, '会发光的种子');
    expect(favorites.single.category, ContentCategory.story);
    expect(favorites.single.ageTiers, <ContentAgeTier>[
      ContentAgeTier.age3to4,
    ]);
    expect(favorites.single.packageVersion, 3);
    expect(favorites.single.available, isTrue);
  });

  test('treats a withdrawn favorite as unavailable', () async {
    final api = HttpContentManagementApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'favorites': <Object?>[
              <String, Object?>{
                'package_id': 'story_001',
                'title': '会发光的种子',
                'category': 'story',
                'age_tiers': <Object?>['age_3_4'],
                'package_version': 3,
                'available': false,
                'created_at': '2026-10-09T08:30:00Z',
              },
            ],
          },
        },
      ),
    );

    final favorites = await api.fetchFavorites();

    expect(favorites.single.available, isFalse);
  });

  test('add and remove favorite target the package path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = HttpContentManagementApi(client);

    await api.addFavorite('story_001');
    await api.removeFavorite('story_001');

    expect(client.putPaths, <String>['/api/v1/content/favorites/story_001']);
    expect(client.deletePaths, <String>[
      '/api/v1/content/favorites/story_001',
    ]);
  });

  test('parses the play-history envelope and forwards the device filter',
      () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'history': <Object?>[
            <String, Object?>{
              'id': 'play-1',
              'device_id': 'sprout_device_001',
              'package_id': 'story_001',
              'package_version': 2,
              'outcome': 'completed',
              'played_seconds': 305,
              'played_at': '2026-10-09T12:00:00Z',
            },
          ],
        },
      },
    );
    final api = HttpContentManagementApi(client);

    final history = await api.fetchPlayHistory(deviceId: 'sprout_device_001');

    expect(history, hasLength(1));
    expect(history.single.deviceId, 'sprout_device_001');
    expect(history.single.outcome, PlayOutcome.completed);
    expect(history.single.playedSeconds, 305);
    expect(client.lastQueryParameters, <String, String>{
      'device_id': 'sprout_device_001',
    });
  });

  test('omits the device filter when listing all devices', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'history': <Object?>[]},
      },
    );
    final api = HttpContentManagementApi(client);

    final history = await api.fetchPlayHistory();

    expect(history, isEmpty);
    expect(client.lastQueryParameters, isEmpty);
  });

  test('parses the theme pack envelope returned by the platform', () async {
    final api = HttpContentManagementApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'theme_packs': <Object?>[
              <String, Object?>{
                'theme_id': 'theme_ocean',
                'title': '海洋主题',
                'summary': '一起探索海底世界',
                'age_tiers': <Object?>['age_3_4'],
                'package_ids': <Object?>['story_001', 'story_002'],
                'catalog_revision': 7,
              },
            ],
          },
        },
      ),
    );

    final packs = await api.fetchThemePacks();

    expect(packs, hasLength(1));
    expect(packs.single.themeId, 'theme_ocean');
    expect(packs.single.title, '海洋主题');
    expect(packs.single.ageTiers, <ContentAgeTier>[ContentAgeTier.age3to4]);
    expect(packs.single.packageIds, <String>['story_001', 'story_002']);
    expect(packs.single.catalogRevision, 7);
  });

  test('parses the recommendation envelope and forwards the age tier',
      () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'recommendations': <Object?>[
            <String, Object?>{
              'package_id': 'story_777',
              'title': '会飞的房子',
              'category': 'story',
              'age_tiers': <Object?>['age_3_4'],
              'score': 6,
              'reason': 'same_category',
            },
          ],
        },
      },
    );
    final api = HttpContentManagementApi(client);

    final result = await api.fetchRecommendations(
      ageTier: ContentAgeTier.age3to4,
      limit: 8,
    );

    expect(result, hasLength(1));
    expect(result.single.packageId, 'story_777');
    expect(result.single.category, ContentCategory.story);
    expect(result.single.reason, ContentRecommendationReason.sameCategory);
    expect(result.single.score, 6);
    expect(client.lastQueryParameters, <String, String>{
      'age_tier': 'age_3_4',
      'limit': '8',
    });
  });

  test('drops recommendations the caller already excluded', () async {
    final api = HttpContentManagementApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'recommendations': <Object?>[
              <String, Object?>{
                'package_id': 'story_777',
                'title': '会飞的房子',
                'category': 'story',
                'age_tiers': <Object?>['age_3_4'],
                'score': 6,
                'reason': 'same_category',
              },
            ],
          },
        },
      ),
    );

    final result = await api.fetchRecommendations(
      excludePackageIds: const <String>['story_777'],
    );

    expect(result, isEmpty);
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  Map<String, String>? lastQueryParameters;
  final List<String> putPaths = <String>[];
  final List<String> deletePaths = <String>[];

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
    deletePaths.add(path);
    return Future<Map<String, Object?>>.value(response);
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
    putPaths.add(path);
    return Future<Map<String, Object?>>.value(response);
  }
}
