import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/ota/device_firmware/data/device_firmware_api.dart';

void main() {
  test('uses the exact device OTA endpoints', () async {
    final client = _RecordingApiClient();
    final api = DeviceFirmwareApi(client);

    await api.fetch('device_demo_001');
    expect(client.calls.last, 'GET /api/v1/devices/device_demo_001/ota');

    await api.install('device_demo_001');
    expect(
      client.calls.last,
      'POST /api/v1/devices/device_demo_001/ota/install',
    );

    await api.status('device_demo_001');
    expect(client.calls.last, 'GET /api/v1/devices/device_demo_001/ota/status');

    await api.retry('device_demo_001');
    expect(client.calls.last, 'POST /api/v1/devices/device_demo_001/ota/retry');

    await api.rollback('device_demo_001');
    expect(
      client.calls.last,
      'POST /api/v1/devices/device_demo_001/ota/rollback',
    );
  });

  test('parses the data envelope returned by the platform', () async {
    final client = _RecordingApiClient();
    final api = DeviceFirmwareApi(client);

    final update = await api.fetch('device_demo_001');

    expect(update.deviceId, 'device_demo_001');
    expect(update.status.wireValue, 'available');
  });
}

class _RecordingApiClient implements ApiClient {
  final List<String> calls = <String>[];

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    calls.add('GET $path');
    return _response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    calls.add('POST $path');
    return _response;
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> delete(String path) {
    throw UnimplementedError();
  }

  Map<String, Object?> get _response => <String, Object?>{
    'data': <String, Object?>{
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
    },
  };
}
