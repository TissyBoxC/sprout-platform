import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_favorites_controller.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  test('loads bookmarks from the platform on first build', () async {
    final api = _FakeContentManagementApi(
      favorites: [
        _favorite('story_001', '会发光的种子'),
      ],
    );
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(
      contentFavoritesControllerProvider.future,
    );

    expect(state.favorites, hasLength(1));
    expect(state.isFavorite('story_001'), isTrue);
    expect(state.isFavorite('story_002'), isFalse);
    expect(api.fetchCount, 1);
  });

  test('toggle adds then removes a bookmark and reconciles', () async {
    final api = _FakeContentManagementApi(favorites: const []);
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(contentFavoritesControllerProvider.future);
    final controller = container.read(
      contentFavoritesControllerProvider.notifier,
    );

    await controller.toggle('story_001');

    expect(api.added, <String>['story_001']);
    expect(
      container.read(contentFavoritesControllerProvider).value?.isFavorite(
        'story_001',
      ),
      isTrue,
    );

    await controller.toggle('story_001');

    expect(api.removed, <String>['story_001']);
    expect(
      container.read(contentFavoritesControllerProvider).value?.isFavorite(
        'story_001',
      ),
      isFalse,
    );
  });

  test('a failed toggle keeps the previous state and reports an error', () async {
    final api = _FakeContentManagementApi(
      favorites: const [],
      failAdd: true,
    );
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(contentFavoritesControllerProvider.future);
    final controller = container.read(
      contentFavoritesControllerProvider.notifier,
    );

    await controller.toggle('story_001');

    final state = container.read(contentFavoritesControllerProvider).value;
    expect(state?.isFavorite('story_001'), isFalse);
    expect(state?.isPending('story_001'), isFalse);
    expect(state?.errorMessage, isNotNull);
  });
}

ContentFavorite _favorite(String packageId, String title) {
  return ContentFavorite(
    packageId: packageId,
    title: title,
    category: ContentCategory.story,
    ageTiers: const [ContentAgeTier.age3to4],
    packageVersion: 1,
    available: true,
    createdAt: DateTime.utc(2026, 10, 9),
  );
}

class _FakeContentManagementApi implements ContentManagementApi {
  _FakeContentManagementApi({
    required List<ContentFavorite> favorites,
    this.failAdd = false,
  }) : _favorites = [...favorites];

  final List<ContentFavorite> _favorites;
  final bool failAdd;
  int fetchCount = 0;
  final List<String> added = <String>[];
  final List<String> removed = <String>[];

  @override
  Future<List<ContentFavorite>> fetchFavorites() async {
    fetchCount += 1;
    return List<ContentFavorite>.unmodifiable(_favorites);
  }

  @override
  Future<void> addFavorite(String packageId) async {
    if (failAdd) {
      throw Exception('network down');
    }
    added.add(packageId);
    _favorites.add(_favorite(packageId, '会发光的种子'));
  }

  @override
  Future<void> removeFavorite(String packageId) async {
    removed.add(packageId);
    _favorites.removeWhere((favorite) => favorite.packageId == packageId);
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
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId}) async =>
      const <ContentPlayRecord>[];

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async =>
      const <ContentThemePack>[];
}
