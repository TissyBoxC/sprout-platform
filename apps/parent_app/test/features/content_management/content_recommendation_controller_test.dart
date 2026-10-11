import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/application/content_recommendation_controller.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  test('loads recommendations and passes the catalog filter through', () async {
    final api = _FakeContentManagementApi(
      recommendations: [_recommendation('story_777')],
    );
    final container = ProviderContainer(
      overrides: [
        contentManagementApiProvider.overrideWithValue(api),
        contentLibraryControllerProvider.overrideWith(
          () => _FakeContentLibraryController(
            ContentLibraryState(
              entries: [
                ContentLibraryEntry(
                  package: _package('story_001'),
                  status: ContentDownloadStatus.notDownloaded,
                  downloadProgress: 0,
                  errorMessage: null,
                ),
              ],
              catalogRevision: 1,
              filter: const ContentLibraryFilter(
                ageTier: ContentAgeTier.age3to4,
              ),
              hasLoadedOnce: true,
            ),
          ),
        ),
      ],
    );
    addTearDown(container.dispose);

    final result = await container.read(
      contentRecommendationControllerProvider.future,
    );

    expect(result, hasLength(1));
    expect(result.single.packageId, 'story_777');
    expect(api.lastAgeTier, ContentAgeTier.age3to4);
    expect(api.lastExclude, <String>['story_001']);
  });

  test('refresh replaces the cached recommendations', () async {
    final api = _FakeContentManagementApi(recommendations: const []);
    final container = ProviderContainer(
      overrides: [
        contentManagementApiProvider.overrideWithValue(api),
        contentLibraryControllerProvider.overrideWith(
          () => _FakeContentLibraryController(
            const ContentLibraryState.initial(),
          ),
        ),
      ],
    );
    addTearDown(container.dispose);

    await container.read(contentRecommendationControllerProvider.future);
    api.recommendations = [_recommendation('story_888')];
    await container
        .read(contentRecommendationControllerProvider.notifier)
        .refresh();

    final result = container
        .read(contentRecommendationControllerProvider)
        .value;
    expect(result, hasLength(1));
    expect(result!.single.packageId, 'story_888');
  });
}

ContentRecommendation _recommendation(String packageId) {
  return ContentRecommendation(
    packageId: packageId,
    title: '会飞的房子',
    category: ContentCategory.story,
    ageTiers: const [ContentAgeTier.age3to4],
    score: 6,
    reason: ContentRecommendationReason.sameCategory,
  );
}

ContentPackage _package(String packageId) {
  return ContentPackage(
    packageId: packageId,
    packageVersion: 1,
    title: '会发光的种子',
    category: ContentCategory.story,
    ageTiers: const [ContentAgeTier.age3to4],
    assetKey: packageId,
    sha256: 'a' * 64,
    sizeBytes: 1024,
    publishedAt: DateTime.utc(2026, 10, 8),
  );
}

class _FakeContentLibraryController extends ContentLibraryController {
  _FakeContentLibraryController(this.value);

  final ContentLibraryState value;

  @override
  Future<ContentLibraryState> build() async => value;
}

class _FakeContentManagementApi implements ContentManagementApi {
  _FakeContentManagementApi({required this.recommendations});

  List<ContentRecommendation> recommendations;
  ContentAgeTier? lastAgeTier;
  List<String> lastExclude = const <String>[];

  @override
  Future<List<ContentRecommendation>> fetchRecommendations({
    ContentAgeTier? ageTier,
    int limit = 8,
    List<String> excludePackageIds = const <String>[],
  }) async {
    lastAgeTier = ageTier;
    lastExclude = excludePackageIds;
    return List<ContentRecommendation>.unmodifiable(recommendations);
  }

  @override
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId) {
    throw UnimplementedError();
  }

  @override
  Future<List<ContentFavorite>> fetchFavorites() async =>
      const <ContentFavorite>[];

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
}
