import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';
import 'package:parent_app/features/content_management/presentation/content_library_page.dart';

void main() {
  testWidgets('shows a branded empty state when no content is available', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          contentLibraryControllerProvider.overrideWith(
            () => _FakeContentLibraryController(
              const ContentLibraryState.initial(),
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
        ],
        child: const MaterialApp(home: ContentLibraryPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('会发光的种子'), findsOneWidget);
    expect(find.text('已下载'), findsOneWidget);
    expect(find.text('故事 · 3～4 岁 · 1.0 MB'), findsOneWidget);
  });
}

class _FakeContentLibraryController extends ContentLibraryController {
  _FakeContentLibraryController(this.value);

  final ContentLibraryState value;

  @override
  Future<ContentLibraryState> build() async => value;
}
