import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/application/content_library_store.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  test(
    'merges an incremental catalog and removes a withdrawn package',
    () async {
      final supportDirectory = await Directory.systemTemp.createTemp(
        'sprout-content-controller-test-',
      );
      addTearDown(() async {
        if (await supportDirectory.exists()) {
          await supportDirectory.delete(recursive: true);
        }
      });
      final store = ContentLibraryStore(
        supportDirectory: () async => supportDirectory,
        temporaryDirectory: () async => supportDirectory,
      );
      final downloadedFile = File(
        '${supportDirectory.path}/content_library/packages/story_001-1.bin',
      );
      await downloadedFile.create(recursive: true);
      await downloadedFile.writeAsBytes([1, 2, 3]);
      await store.writeIndex(
        ContentLibraryIndex(
          catalogRevision: 4,
          packages: {
            'story_001': DownloadedContent(
              packageId: 'story_001',
              packageVersion: 1,
              sha256: 'a' * 64,
              relativePath: 'packages/story_001-1.bin',
              downloadedAt: DateTime.utc(2026, 10, 8),
            ),
          },
        ),
      );
      final api = _FakeContentManagementApi(
        catalog: const ContentCatalog(
          catalogRevision: 5,
          packages: [],
          withdrawnPackageIds: ['story_001'],
        ),
      );
      final container = ProviderContainer(
        overrides: [
          contentManagementApiProvider.overrideWithValue(api),
          contentLibraryStoreProvider.overrideWithValue(store),
        ],
      );
      addTearDown(container.dispose);

      final state = await container.read(
        contentLibraryControllerProvider.future,
      );

      expect(state.catalogRevision, 5);
      expect(state.entries, isEmpty);
      expect(await downloadedFile.exists(), isFalse);
      final index = await store.loadIndex();
      expect(index.packages, isEmpty);
    },
  );

  test('replaces the remote catalog when the age filter changes', () async {
    final supportDirectory = await Directory.systemTemp.createTemp(
      'sprout-content-filter-test-',
    );
    addTearDown(() async {
      if (await supportDirectory.exists()) {
        await supportDirectory.delete(recursive: true);
      }
    });
    final store = ContentLibraryStore(
      supportDirectory: () async => supportDirectory,
      temporaryDirectory: () async => supportDirectory,
    );
    final api = _FilteringContentManagementApi();
    final container = ProviderContainer(
      overrides: [
        contentManagementApiProvider.overrideWithValue(api),
        contentLibraryStoreProvider.overrideWithValue(store),
      ],
    );
    addTearDown(container.dispose);

    await container.read(contentLibraryControllerProvider.future);
    final controller = container.read(
      contentLibraryControllerProvider.notifier,
    );
    await controller.applyFilter(
      const ContentLibraryFilter(ageTier: ContentAgeTier.age5to6),
    );
    await controller.refresh();

    final state = container.read(contentLibraryControllerProvider).value;
    expect(state?.entries.map((entry) => entry.package.packageId), ['age_5_6']);
    expect(api.lastAgeTier, ContentAgeTier.age5to6);
    expect(api.sinceRevisions.last, 1);
  });
}

class _FakeContentManagementApi implements ContentManagementApi {
  _FakeContentManagementApi({required this.catalog});

  final ContentCatalog catalog;

  @override
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  }) async {
    return catalog;
  }

  @override
  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId) {
    throw UnimplementedError();
  }

  @override
  Future<List<ContentFavorite>> fetchFavorites() async => const [];

  @override
  Future<void> addFavorite(String packageId) async {}

  @override
  Future<void> removeFavorite(String packageId) async {}

  @override
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId}) async =>
      const <ContentPlayRecord>[];

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async =>
      const <ContentThemePack>[];

  @override
  Future<List<ContentRecommendation>> fetchRecommendations({
    ContentAgeTier? ageTier,
    int limit = 8,
    List<String> excludePackageIds = const <String>[],
  }) async => const <ContentRecommendation>[];
}

class _FilteringContentManagementApi implements ContentManagementApi {
  ContentAgeTier? lastAgeTier;
  final List<int> sinceRevisions = [];

  @override
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  }) async {
    lastAgeTier = ageTier;
    sinceRevisions.add(sinceRevision);
    final packages = ageTier == null
        ? [
            _contentPackage('age_3_4', const [ContentAgeTier.age3to4]),
          ]
        : [
            _contentPackage('age_5_6', const [ContentAgeTier.age5to6]),
          ];
    return ContentCatalog(
      catalogRevision: sinceRevision + 1,
      packages: packages,
      withdrawnPackageIds: const [],
    );
  }

  @override
  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId) {
    throw UnimplementedError();
  }

  @override
  Future<List<ContentFavorite>> fetchFavorites() async => const [];

  @override
  Future<void> addFavorite(String packageId) async {}

  @override
  Future<void> removeFavorite(String packageId) async {}

  @override
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId}) async =>
      const [];

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async => const [];

  @override
  Future<List<ContentRecommendation>> fetchRecommendations({
    ContentAgeTier? ageTier,
    int limit = 8,
    List<String> excludePackageIds = const <String>[],
  }) async => const [];
}

ContentPackage _contentPackage(
  String packageId,
  List<ContentAgeTier> ageTiers,
) {
  return ContentPackage(
    packageId: packageId,
    packageVersion: 1,
    title: packageId,
    category: ContentCategory.story,
    ageTiers: ageTiers,
    assetKey: packageId,
    sha256: 'a' * 64,
    sizeBytes: 1024,
    publishedAt: DateTime.utc(2026, 10, 8),
  );
}
