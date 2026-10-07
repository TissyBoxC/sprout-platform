import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  group('ContentCatalog.fromJson', () {
    test('parses added and updated packages', () {
      final catalog = ContentCatalog.fromJson(<String, Object?>{
        'catalog_revision': 12,
        'packages': [
          _packageJson(
            packageId: 'story_001',
            packageVersion: 1,
            sha256: 'a' * 64,
          ),
          _packageJson(
            packageId: 'story_001',
            packageVersion: 2,
            sha256: 'b' * 64,
            title: '会发光的种子（新版）',
          ),
        ],
        'withdrawn_package_ids': <Object?>[],
      });

      expect(catalog.catalogRevision, 12);
      expect(catalog.packages, hasLength(2));
      expect(catalog.packages[0].packageId, 'story_001');
      expect(catalog.packages[0].packageVersion, 1);
      expect(catalog.packages[1].packageVersion, 2);
      expect(catalog.withdrawnPackageIds, isEmpty);
    });

    test('rejects a non-numeric package version from an older contract', () {
      expect(
        () => ContentCatalog.fromJson(<String, Object?>{
          'catalog_revision': 1,
          'packages': [
            _packageJson(
              packageId: 'story_001',
              packageVersion: '1.0.0',
              sha256: 'a' * 64,
            ),
          ],
          'withdrawn_package_ids': <Object?>[],
        }),
        throwsA(isA<FormatException>()),
      );
    });

    test('parses withdrawn package ids without dropping them', () {
      final catalog = ContentCatalog.fromJson(<String, Object?>{
        'catalog_revision': 13,
        'packages': <Object?>[],
        'withdrawn_package_ids': <Object?>['story_001', 'poem_002'],
      });

      expect(catalog.catalogRevision, 13);
      expect(catalog.packages, isEmpty);
      expect(catalog.withdrawnPackageIds, <String>['story_001', 'poem_002']);
    });

    test('rejects unknown categories instead of showing a broken row', () {
      expect(
        () => ContentCatalog.fromJson(<String, Object?>{
          'catalog_revision': 1,
          'packages': [
            _packageJson(
              packageId: 'unknown_001',
              packageVersion: 1,
              sha256: 'a' * 64,
              category: 'unknown',
            ),
          ],
          'withdrawn_package_ids': <Object?>[],
        }),
        throwsA(isA<FormatException>()),
      );
    });
  });
}

Map<String, Object?> _packageJson({
  required String packageId,
  required Object packageVersion,
  required String sha256,
  String category = 'story',
  String title = '会发光的种子',
}) {
  return <String, Object?>{
    'package_id': packageId,
    'package_version': packageVersion,
    'title': title,
    'category': category,
    'age_tiers': <Object?>['age_3_4', 'age_5_6'],
    'asset_key': 'content/$packageId/$packageVersion',
    'sha256': sha256,
    'size_bytes': 1048576,
    'published_at': '2026-10-08T00:00:00Z',
  };
}
