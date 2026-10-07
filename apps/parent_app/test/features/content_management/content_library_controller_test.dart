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
}
