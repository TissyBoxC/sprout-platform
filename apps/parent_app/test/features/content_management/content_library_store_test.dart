import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/content_management/application/content_library_store.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';
import 'package:path/path.dart' as path;

void main() {
  group('ContentLibraryStore index', () {
    test('loads an empty index when no file exists yet', () async {
      final supportDirectory = await Directory.systemTemp.createTemp(
        'sprout-content-test-',
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

      final index = await store.loadIndex();

      expect(index.catalogRevision, 0);
      expect(index.packages, isEmpty);
    });

    test('writes and reloads the index without a partial file', () async {
      final supportDirectory = await Directory.systemTemp.createTemp(
        'sprout-content-test-',
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
      final index = ContentLibraryIndex(
        catalogRevision: 8,
        packages: {
          'story_001': DownloadedContent(
            packageId: 'story_001',
            packageVersion: 1,
            sha256: 'a' * 64,
            relativePath: 'packages/story_001-1.bin',
            downloadedAt: DateTime.utc(2026, 10, 8),
          ),
        },
      );

      await store.writeIndex(index);
      final loaded = await store.loadIndex();
      final root = Directory(
        path.join(supportDirectory.path, 'content_library'),
      );
      final stagingFiles = await root
          .list(followLinks: false)
          .where((entity) => path.basename(entity.path).startsWith('._index'))
          .toList();

      expect(loaded.catalogRevision, 8);
      expect(loaded.packages['story_001']?.packageVersion, 1);
      expect(stagingFiles, isEmpty);
    });
  });

  group('ContentLibraryStore integrity', () {
    test('deletes a temporary file when SHA-256 does not match', () async {
      final testDirectory = await Directory.systemTemp.createTemp(
        'sprout-content-test-',
      );
      addTearDown(() async {
        if (await testDirectory.exists()) {
          await testDirectory.delete(recursive: true);
        }
      });
      final packageFile = File(path.join(testDirectory.path, 'package.part'));
      await packageFile.writeAsBytes([1, 2, 3, 4]);

      await expectLater(
        ContentLibraryStore.verifySha256(packageFile, 'A' * 64),
        throwsA(
          isA<AppException>().having(
            (error) => error.message,
            'message',
            '内容校验没有通过，请稍后重试',
          ),
        ),
      );
      expect(await packageFile.exists(), isFalse);
    });
  });
}
