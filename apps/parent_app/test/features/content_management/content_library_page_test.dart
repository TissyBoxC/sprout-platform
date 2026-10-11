import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_favorites_controller.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/application/content_play_history_controller.dart';
import 'package:parent_app/features/content_management/application/content_theme_pack_controller.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';
import 'package:parent_app/features/content_management/presentation/content_library_page.dart';

void main() {
  testWidgets('shows a branded empty state when no content is available', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentPlayHistoryControllerProvider.overrideWith(
            () => _FakeContentPlayHistoryController(
              const ContentPlayHistoryState.initial(),
            ),
          ),
          contentThemePackControllerProvider.overrideWith(
            () => _FakeContentThemePackController(const <ContentThemePack>[]),
          ),
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              const ContentLibraryState.initial(),
            ),
          ),
          contentFavoritesControllerProvider.overrideWith(
            () => _FakeContentFavoritesController(
              const ContentFavoritesState.initial(),
            ),
          ),
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pump();

    expect(find.text('内容管理'), findsOneWidget);
    expect(find.text('这里还没有适合的内容'), findsOneWidget);
    expect(find.text('换个分类或年龄试试，也可以稍后下拉刷新。'), findsOneWidget);
  });

  testWidgets('shows the package title and local download state', (
    tester,
  ) async {
    final package = ContentPackage(
      packageId: 'story_001',
      packageVersion: 1,
      title: '会发光的种子',
      category: ContentCategory.story,
      ageTiers: const [ContentAgeTier.age3to4],
      assetKey: 'story_001',
      sha256: 'a' * 64,
      sizeBytes: 1048576,
      publishedAt: DateTime.utc(2026, 10, 8),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentPlayHistoryControllerProvider.overrideWith(
            () => _FakeContentPlayHistoryController(
              const ContentPlayHistoryState.initial(),
            ),
          ),
          contentThemePackControllerProvider.overrideWith(
            () => _FakeContentThemePackController(const <ContentThemePack>[]),
          ),
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              ContentLibraryState(
                entries: [
                  ContentLibraryEntry(
                    package: package,
                    status: ContentDownloadStatus.downloaded,
                    downloadProgress: 1,
                    errorMessage: null,
                  ),
                ],
                catalogRevision: 1,
                filter: const ContentLibraryFilter(),
                hasLoadedOnce: true,
              ),
            ),
          ),
          contentFavoritesControllerProvider.overrideWith(
            () => _FakeContentFavoritesController(
              const ContentFavoritesState.initial(),
            ),
          ),
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('会发光的种子'), findsOneWidget);
    expect(find.text('已下载'), findsOneWidget);
    expect(find.text('故事 · 3～4 岁 · 1.0 MB'), findsOneWidget);
  });

  testWidgets('label a bookmarked package with a filled star', (tester) async {
    final package = ContentPackage(
      packageId: 'story_001',
      packageVersion: 1,
      title: '会发光的种子',
      category: ContentCategory.story,
      ageTiers: const [ContentAgeTier.age3to4],
      assetKey: 'story_001',
      sha256: 'a' * 64,
      sizeBytes: 1048576,
      publishedAt: DateTime.utc(2026, 10, 8),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentPlayHistoryControllerProvider.overrideWith(
            () => _FakeContentPlayHistoryController(
              const ContentPlayHistoryState.initial(),
            ),
          ),
          contentThemePackControllerProvider.overrideWith(
            () => _FakeContentThemePackController(const <ContentThemePack>[]),
          ),
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              ContentLibraryState(
                entries: [
                  ContentLibraryEntry(
                    package: package,
                    status: ContentDownloadStatus.notDownloaded,
                    downloadProgress: 0,
                    errorMessage: null,
                  ),
                ],
                catalogRevision: 1,
                filter: const ContentLibraryFilter(),
                hasLoadedOnce: true,
              ),
            ),
          ),
          contentFavoritesControllerProvider.overrideWith(
            () => _FakeContentFavoritesController(
              ContentFavoritesState(
                favorites: [
                  ContentFavorite(
                    packageId: 'story_001',
                    title: '会发光的种子',
                    category: ContentCategory.story,
                    ageTiers: const [ContentAgeTier.age3to4],
                    packageVersion: 1,
                    available: true,
                    createdAt: DateTime.utc(2026, 10, 8),
                  ),
                ],
                pendingPackageIds: const <String>{},
                hasLoadedOnce: true,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.star_rounded), findsWidgets);
    expect(find.byTooltip('取消收藏'), findsOneWidget);
  });

  testWidgets('renders the favorites tab with bookmarked packages', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentPlayHistoryControllerProvider.overrideWith(
            () => _FakeContentPlayHistoryController(
              const ContentPlayHistoryState.initial(),
            ),
          ),
          contentThemePackControllerProvider.overrideWith(
            () => _FakeContentThemePackController(const <ContentThemePack>[]),
          ),
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              const ContentLibraryState.initial(),
            ),
          ),
          contentFavoritesControllerProvider.overrideWith(
            () => _FakeContentFavoritesController(
              ContentFavoritesState(
                favorites: [
                  ContentFavorite(
                    packageId: 'poem_009',
                    title: '静夜思',
                    category: ContentCategory.poetry,
                    ageTiers: const [ContentAgeTier.age5to6],
                    packageVersion: 2,
                    available: true,
                    createdAt: DateTime.utc(2026, 10, 9),
                  ),
                ],
                pendingPackageIds: const <String>{},
                hasLoadedOnce: true,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('我的收藏'));
    await tester.pumpAndSettle();

    expect(find.text('静夜思'), findsOneWidget);
    expect(find.text('古诗 · 5～6 岁'), findsOneWidget);
  });

  testWidgets('renders the playback history tab with records', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentPlayHistoryControllerProvider.overrideWith(
            () => _FakeContentPlayHistoryController(
              ContentPlayHistoryState(
                records: [
                  ContentPlayRecord(
                    id: 'play-1',
                    deviceId: 'sprout_device_001',
                    packageId: 'story_001',
                    packageVersion: 1,
                    outcome: PlayOutcome.completed,
                    playedSeconds: 305,
                    playedAt: DateTime.utc(2026, 10, 9, 12),
                  ),
                ],
              ),
            ),
          ),
          contentThemePackControllerProvider.overrideWith(
            () => _FakeContentThemePackController(const <ContentThemePack>[]),
          ),
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              const ContentLibraryState.initial(),
            ),
          ),
          contentFavoritesControllerProvider.overrideWith(
            () => _FakeContentFavoritesController(
              const ContentFavoritesState.initial(),
            ),
          ),
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('播放记录'));
    await tester.pumpAndSettle();

    expect(find.text('story_001'), findsOneWidget);
    expect(find.text('已听完'), findsOneWidget);
    expect(find.textContaining('5 分 5 秒'), findsOneWidget);
  });

  testWidgets('renders published theme packs on the themes tab', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentThemePackControllerProvider.overrideWith(
            () => _FakeContentThemePackController(const [
              ContentThemePack(
                themeId: 'theme_ocean',
                title: '海洋主题',
                summary: '一起探索海底世界',
                ageTiers: [ContentAgeTier.age3to4],
                packageIds: ['story_001', 'story_002'],
                catalogRevision: 7,
              ),
            ]),
          ),
          contentPlayHistoryControllerProvider.overrideWith(
            () => _FakeContentPlayHistoryController(
              const ContentPlayHistoryState.initial(),
            ),
          ),
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              const ContentLibraryState.initial(),
            ),
          ),
          contentFavoritesControllerProvider.overrideWith(
            () => _FakeContentFavoritesController(
              const ContentFavoritesState.initial(),
            ),
          ),
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('主题'));
    await tester.pumpAndSettle();

    expect(find.text('海洋主题'), findsOneWidget);
    expect(find.text('一起探索海底世界'), findsOneWidget);
    expect(find.text('2 个内容'), findsOneWidget);
  });
}

class _FakeContentLibraryController extends ContentLibraryController {
  _FakeContentLibraryController(this.value);

  final ContentLibraryState value;

  @override
  Future<ContentLibraryState> build() async => value;
}

class _FakeContentFavoritesController extends ContentFavoritesController {
  _FakeContentFavoritesController(this.value);

  final ContentFavoritesState value;

  @override
  Future<ContentFavoritesState> build() async => value;
}

class _FakeContentPlayHistoryController extends ContentPlayHistoryController {
  _FakeContentPlayHistoryController(this.value);

  final ContentPlayHistoryState value;

  @override
  Future<ContentPlayHistoryState> build() async => value;
}

class _FakeContentThemePackController extends ContentThemePackController {
  _FakeContentThemePackController(this.value);

  final List<ContentThemePack> value;

  @override
  Future<List<ContentThemePack>> build() async => value;
}
