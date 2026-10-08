import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/settings/application/privacy_export_store.dart';

void main() {
  test('writes a readable export to application documents storage', () async {
    final directory = await Directory.systemTemp.createTemp(
      'sprout-privacy-export-test-',
    );
    addTearDown(() async {
      if (await directory.exists()) {
        await directory.delete(recursive: true);
      }
    });
    final store = PrivacyExportStore(documentsDirectory: () async => directory);

    final exported = await store.save(const {
      'account': {'phone': '13800138000'},
      'children': <Object?>[],
      'devices': <Object?>[],
    }, now: DateTime(2026, 10, 8, 21, 5, 9));

    final file = File(exported.filePath);
    expect(await file.exists(), isTrue);
    expect(exported.fileName, 'sprout-privacy-20261008-210509.json');
    expect(exported.sizeBytes, greaterThan(0));
    final decoded = jsonDecode(await file.readAsString());
    expect(decoded, isA<Map<String, Object?>>());
    expect((decoded as Map<String, Object?>)['account'], {
      'phone': '13800138000',
    });
  });

  test('removes credential-shaped fields recursively', () {
    final payload = redactPrivacyExport({
      'account': {
        'phone': '13800138000',
        'password': 'not-for-export',
        'nested': {
          'accessToken': 'secret-token',
          'session_id': 'session',
          'safe': 'keep-me',
        },
      },
      'devices': [
        {'device_id': 'sprout-001', 'api_key': 'key'},
        {'device_id': 'sprout-002', 'nonce': 'nonce'},
      ],
      'password_hash': 'hash',
    });

    final encoded = jsonEncode(payload);
    expect(encoded, contains('13800138000'));
    expect(encoded, contains('keep-me'));
    expect(encoded, contains('sprout-001'));
    expect(encoded, isNot(contains('not-for-export')));
    expect(encoded, isNot(contains('secret-token')));
    expect(encoded, isNot(contains('session')));
    expect(encoded, isNot(contains('key')));
    expect(encoded, isNot(contains('nonce')));
    expect(encoded, isNot(contains('hash')));
  });
}
