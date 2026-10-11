import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/application/content_theme_pack_controller.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  test('loads the published theme packs on first build', () async {
    final api = _FakeContentManagementApi(
      themePacks: [_themePack('theme_ocean', '海洋主题')],
    );
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final packs = await container.read(
      contentThemePackControllerProvider.future,
    );

    expect(packs, hasLength(1));
    expect(packs.single.themeId, 'theme_ocean');
  });

  test('refresh replaces the cached theme packs', () async {
    final api = _FakeContentManagementApi(themePacks: const []);
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(contentThemePackControllerProvider.future);
    api.themePacks = [_themePack('theme_night', '睡前一周')];
    await container.read(contentThemePackControllerProvider.notifier).refresh();

    final packs = container.read(contentThemePackControllerProvider).value;
    expect(packs, hasLength(1));
    expect(packs!.single.themeId, 'theme_night');
  });
}

ContentThemePack _themePack(String themeId, String title) {
  return ContentThemePack(
    themeId: themeId,
    title: title,
    summary: '',
    ageTiers: const [ContentAgeTier.age3to4],
    packageIds: const ['story_001'],
    catalogRevision: 1,
  );
}

class _FakeContentManagementApi implements ContentManagementApi {
  _FakeContentManagementApi({required this.themePacks});

  List<ContentThemePack> themePacks;

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async =>
      List<ContentThemePack>.unmodifiable(themePacks);

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
}
