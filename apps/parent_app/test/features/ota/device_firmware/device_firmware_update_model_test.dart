import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/ota/device_firmware/domain/device_firmware_update.dart';

void main() {
  group('DeviceFirmwareUpdate', () {
    test('parses a complete device update with release metadata', () {
      final update = DeviceFirmwareUpdate.fromJson(_deviceUpdateJson());

      expect(update.deviceId, 'device_demo_001');
      expect(update.status, DeviceFirmwareUpdateStatus.available);
      expect(update.currentVersion, '0.9.0');
      expect(update.targetVersion, '0.10.0');
      expect(update.updateAvailable, isTrue);
      expect(update.rollbackAvailable, isFalse);
      expect(update.release?.channel, 'stable');
      expect(update.release?.isInstallable, isTrue);
      expect(update.release?.sizeBytes, 4194304);
      expect(update.checkedAt, DateTime.parse('2026-10-09T00:00:00Z'));
    });

    test('rejects an unknown lifecycle value instead of guessing', () {
      final json = _deviceUpdateJson()..['status'] = 'mystery_state';

      expect(() => DeviceFirmwareUpdate.fromJson(json), throwsFormatException);
    });

    test('rejects an invalid sha256 in release metadata', () {
      final release = (_deviceUpdateJson()['release'] as Map<String, Object?>)
          .clone();
      release['sha256'] = 'not-a-checksum';
      final json = _deviceUpdateJson()..['release'] = release;

      expect(() => DeviceFirmwareUpdate.fromJson(json), throwsFormatException);
    });

    test('accepts legacy version while keeping the canonical field first', () {
      final release = (_deviceUpdateJson()['release'] as Map<String, Object?>)
          .clone();
      release.remove('firmware_version');
      release['version'] = '0.10.1';
      final json = _deviceUpdateJson()..['release'] = release;

      final update = DeviceFirmwareUpdate.fromJson(json);

      expect(update.release?.firmwareVersion, '0.10.1');
    });

    test('requires a real signature instead of accepting a checksum', () {
      final release =
          (_deviceUpdateJson()['release'] as Map<String, Object?>).clone()
            ..remove('signature');
      final json = _deviceUpdateJson()..['release'] = release;

      expect(() => DeviceFirmwareUpdate.fromJson(json), throwsFormatException);
    });

    test('accepts optional audience fields being absent', () {
      final release =
          (_deviceUpdateJson()['release'] as Map<String, Object?>).clone()
            ..remove('target_type')
            ..remove('canary_percent')
            ..remove('release_notes');
      final json = _deviceUpdateJson()..['release'] = release;

      final update = DeviceFirmwareUpdate.fromJson(json);

      expect(update.release?.targetType, isNull);
      expect(update.release?.canaryPercent, isNull);
      expect(update.release?.releaseNotes, isNull);
      expect(update.release?.isInstallable, isTrue);
    });

    test('clamps progress and tolerates missing release metadata', () {
      final json = _deviceUpdateJson()
        ..['release'] = null
        ..['progress_percent'] = 180
        ..['target_version'] = null;

      final update = DeviceFirmwareUpdate.fromJson(json);

      expect(update.progressPercent, 100);
      expect(update.release, isNull);
      expect(update.targetVersion, isNull);
    });

    test('exposes retry, rollback, and in-progress state helpers', () {
      expect(DeviceFirmwareUpdateStatus.failed.allowsRetry, isTrue);
      expect(
        DeviceFirmwareUpdateStatus.rollbackAvailable.allowsRollback,
        isTrue,
      );
      expect(DeviceFirmwareUpdateStatus.downloading.isInProgress, isTrue);
      expect(DeviceFirmwareUpdateStatus.succeeded.isInProgress, isFalse);
    });
  });
}

Map<String, Object?> _deviceUpdateJson() {
  return <String, Object?>{
    'schema_version': '1.0.0',
    'device_id': 'device_demo_001',
    'deployment_id': 'ota_deployment_001',
    'status': 'available',
    'current_version': '0.9.0',
    'target_version': '0.10.0',
    'update_available': true,
    'rollback_available': false,
    'progress_percent': 0,
    'checked_at': '2026-10-09T00:00:00Z',
    'release': <String, Object?>{
      'schema_version': '1.0.0',
      'release_id': 'ota_release_001',
      'firmware_version': '0.10.0',
      'hardware_revision': 's3_n16r8',
      'channel': 'stable',
      'status': 'published',
      'artifact_key': '0.10.0/stable/esp32_s3/firmware/sprout.bin',
      'artifact_url':
          'https://api.clarkhub.cn/releases/0.10.0/stable/esp32_s3/firmware/sprout.bin',
      'sha256':
          'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      'size_bytes': 4194304,
      'signature_key_id': 'ota_signing_001',
      'signature': 'MEUCIQDExampleSignatureForContractValidationOnly',
      'rollback_allowed': true,
      'min_source_version': '0.9.0',
      'published_at': '2026-10-09T00:00:00Z',
      'canary_percent': 10,
      'target_type': 'all',
      'target_id': '',
      'release_notes': '提升设备更新稳定性。',
      'created_at': '2026-10-08T00:00:00Z',
      'updated_at': '2026-10-09T00:00:00Z',
    },
  };
}

extension on Map<String, Object?> {
  Map<String, Object?> clone() => Map<String, Object?>.from(this);
}
