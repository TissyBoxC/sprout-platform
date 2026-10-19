import 'dart:io';

import 'package:archive/archive.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/ota/application/app_update_service.dart';
import 'package:parent_app/features/ota/application/resource_archive_extractor.dart';
import 'package:path/path.dart' as path;

void main() {
  group('AppUpdateService.updateProblem', () {
    test('accepts a complete HTTPS package specification', () {
      final service = AppUpdateService();

      expect(service.updateProblem(_update()), isNull);
    });

    test('rejects a non-HTTPS download address', () {
      final service = AppUpdateService();

      expect(
        service.updateProblem(_update(downloadUrl: 'http://example.com/a.zip')),
        '更新地址不安全，请联系客服处理',
      );
    });

    test('rejects a missing checksum', () {
      final service = AppUpdateService();

      expect(service.updateProblem(_update(sha256: '')), '更新校验信息不完整，请稍后再试');
    });
  });

  group('installed resource version reconciliation', () {
    test('same installed resource version is not offered again', () {
      final result = reconcileInstalledUpdate(
        candidate: _update(),
        installed: const AppUpdateVersions(
          clientVersion: '1.0.0',
          resourceVersion: '1.0.1',
        ),
      );

      expect(result, isNull);
    });

    test('higher resource version remains available', () {
      final result = reconcileInstalledUpdate(
        candidate: _update(),
        installed: const AppUpdateVersions(
          clientVersion: '1.0.0',
          resourceVersion: '1.0.0',
        ),
      );

      expect(result?.version, '1.0.1');
    });

    test('higher client version remains available despite resource marker', () {
      const clientUpdate = AppUpdateInfo(
        kind: 'client',
        version: '1.0.1',
        downloadUrl: 'https://example.com/app.apk',
        sha256:
            'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
            'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        releaseNotes: '客户端更新',
        isMandatory: false,
        minSupportedVersion: '1.0.0',
      );
      final result = reconcileInstalledUpdate(
        candidate: clientUpdate,
        installed: const AppUpdateVersions(
          clientVersion: '1.0.0',
          resourceVersion: '1.0.1',
        ),
      );

      expect(result?.isClientUpdate, isTrue);
    });

    test('same client version is not offered again despite a resource marker', () {
      const clientUpdate = AppUpdateInfo(
        kind: 'client',
        version: '1.0.1',
        downloadUrl: 'https://example.com/app.apk',
        sha256:
            'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
            'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        releaseNotes: '客户端更新',
        isMandatory: false,
        minSupportedVersion: '1.0.0',
      );
      final result = reconcileInstalledUpdate(
        candidate: clientUpdate,
        installed: const AppUpdateVersions(
          clientVersion: '1.0.1',
          resourceVersion: '0.9.0',
        ),
      );

      expect(result, isNull);
    });

    test('without a resource marker the server decision is preserved', () {
      final result = reconcileInstalledUpdate(
        candidate: _update(),
        installed: const AppUpdateVersions(
          clientVersion: '1.0.0',
          resourceVersion: '',
        ),
      );

      expect(result, isNotNull);
    });

    test('compares numeric, pre-release and build metadata versions', () {
      expect(compareAppVersions('1.10.0', '1.9.9'), greaterThan(0));
      expect(compareAppVersions('1.0.1', '1.0.1+build.2'), 0);
      expect(compareAppVersions('1.0.1-beta.2', '1.0.1-beta.3'), lessThan(0));
      expect(compareAppVersions('1.0.1-beta.3', '1.0.1'), lessThan(0));
      expect(compareAppVersions('1.0.1', '1.0.1'), 0);
    });
  });

  test('installed resource marker is read only from the active file', () async {
    final directory = await Directory.systemTemp.createTemp(
      'sprout-resource-marker-test-',
    );
    addTearDown(() async {
      if (await directory.exists()) {
        await directory.delete(recursive: true);
      }
    });
    final marker = File(path.join(directory.path, 'current_version'));

    expect(await AppUpdateService.readInstalledResourceVersion(marker), '');
    await marker.writeAsString('  1.2.3  ');
    expect(
      await AppUpdateService.readInstalledResourceVersion(marker),
      '1.2.3',
    );
    await marker.writeAsString('   ');
    expect(await AppUpdateService.readInstalledResourceVersion(marker), '');
  });

  group('resource archive boundary', () {
    test('rejects a traversing archive entry before writing it', () async {
      final supportDirectory = await Directory.systemTemp.createTemp(
        'sprout-update-test-',
      );
      addTearDown(() async {
        if (await supportDirectory.exists()) {
          await supportDirectory.delete(recursive: true);
        }
      });

      final archive = Archive()
        ..add(ArchiveFile.string('../outside.txt', 'do not write'));
      final archiveBytes = ZipEncoder().encode(archive);
      final packageFile = File('${supportDirectory.path}/package.zip');
      await packageFile.writeAsBytes(archiveBytes);
      final destination = Directory('${supportDirectory.path}/unpacked')
        ..createSync();

      await expectLater(
        ResourceArchiveExtractor().extract(
          archiveFile: packageFile,
          destination: destination,
        ),
        throwsA(isA<AppException>()),
      );
      expect(
        File('${supportDirectory.path}/outside.txt').existsSync(),
        isFalse,
      );
    });

    test('extracts a normal archive inside the destination', () async {
      final supportDirectory = await Directory.systemTemp.createTemp(
        'sprout-update-test-',
      );
      addTearDown(() async {
        if (await supportDirectory.exists()) {
          await supportDirectory.delete(recursive: true);
        }
      });

      final archive = Archive()
        ..add(ArchiveFile.string('content/story.txt', 'new story'));
      final archiveBytes = ZipEncoder().encode(archive);
      final packageFile = File('${supportDirectory.path}/package.zip');
      await packageFile.writeAsBytes(archiveBytes);
      final destination = Directory('${supportDirectory.path}/unpacked')
        ..createSync();

      await ResourceArchiveExtractor().extract(
        archiveFile: packageFile,
        destination: destination,
      );

      expect(
        File('${destination.path}/content/story.txt').readAsStringSync(),
        'new story',
      );
    });
  });

  group('AppUpdateService client update files', () {
    test(
      'removes only expired APKs and preserves the current update',
      () async {
        final updatesDirectory = await Directory.systemTemp.createTemp(
          'sprout-update-test-',
        );
        addTearDown(() async {
          if (await updatesDirectory.exists()) {
            await updatesDirectory.delete(recursive: true);
          }
        });

        final now = DateTime(2026, 10, 4, 12);
        final expiredUpdateFile = File(
          path.join(updatesDirectory.path, 'sprout-update-1.0.1-old.apk'),
        );
        final currentUpdateFile = File(
          path.join(updatesDirectory.path, 'sprout-update-1.0.2-current.apk'),
        );
        final recentUpdateFile = File(
          path.join(updatesDirectory.path, 'sprout-update-1.0.3-recent.apk'),
        );
        final retainedFile = File(path.join(updatesDirectory.path, 'keep.txt'));
        await Future.wait([
          expiredUpdateFile.writeAsBytes([1]),
          currentUpdateFile.writeAsBytes([2]),
          recentUpdateFile.writeAsBytes([3]),
          retainedFile.writeAsString('keep'),
        ]);
        await expiredUpdateFile.setLastModified(
          now.subtract(const Duration(hours: 48)),
        );
        // Make the current file old enough for cleanup so this test proves the
        // current-path exclusion protects it.
        await currentUpdateFile.setLastModified(
          now.subtract(const Duration(hours: 48)),
        );
        await recentUpdateFile.setLastModified(
          now.subtract(const Duration(hours: 1)),
        );
        await retainedFile.setLastModified(
          now.subtract(const Duration(hours: 48)),
        );

        await AppUpdateService.removeExpiredClientUpdates(
          updatesDirectory: updatesDirectory,
          currentUpdate: currentUpdateFile,
          now: now,
        );

        expect(await expiredUpdateFile.exists(), isFalse);
        expect(await currentUpdateFile.exists(), isTrue);
        expect(await recentUpdateFile.exists(), isTrue);
        expect(await retainedFile.exists(), isTrue);
      },
    );

    test('rejects a file whose SHA-256 does not match', () async {
      final testDirectory = await Directory.systemTemp.createTemp(
        'sprout-update-test-',
      );
      addTearDown(() async {
        if (await testDirectory.exists()) {
          await testDirectory.delete(recursive: true);
        }
      });

      final packageFile = File(path.join(testDirectory.path, 'package.apk'));
      await packageFile.writeAsBytes([1, 2, 3, 4]);

      await expectLater(
        AppUpdateService.verifySha256(packageFile, 'A' * 64),
        throwsA(isA<AppException>()),
      );
    });
  });
}

AppUpdateInfo _update({String? downloadUrl, String? sha256}) {
  return AppUpdateInfo(
    kind: 'resource',
    version: '1.0.1',
    downloadUrl: downloadUrl ?? 'https://example.com/resource.zip',
    sha256: sha256 ?? 'A' * 64,
    releaseNotes: '更新内容',
    isMandatory: false,
    minSupportedVersion: '1.0.0',
  );
}
