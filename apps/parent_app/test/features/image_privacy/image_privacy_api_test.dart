import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/image_privacy/data/image_privacy_api.dart';
import 'package:parent_app/features/image_privacy/domain/image_privacy.dart';

void main() {
  test('parses the policy envelope and editor catalogs', () async {
    final api = HttpImagePrivacyApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'policy': <String, Object?>{
              'schema_version': 1,
              'enabled': true,
              'upload_allowed': false,
              'local_processing_only': true,
              'processing_mode': 'metadata',
              'upload_scope': 'none',
              'retention_days': 7,
              'redaction_level': 'strict',
              'require_child_consent': true,
              'revision': 3,
            },
            'processing_modes': <Object?>['off', 'metadata', 'local_full'],
            'upload_scopes': <Object?>['none', 'thumbnails', 'analysis'],
            'redaction_levels': <Object?>['none', 'standard', 'strict'],
            'block_reasons': <Object?>[
              'none',
              'disabled',
              'consent',
              'scope',
              'quota',
              'error',
            ],
          },
        },
      ),
    );

    final config = await api.fetchPolicy();

    expect(config.policy.schemaVersion, 1);
    expect(config.policy.enabled, isTrue);
    expect(config.policy.localProcessingOnly, isTrue);
    expect(config.policy.processingMode, 'metadata');
    expect(config.policy.retentionDays, 7);
    expect(config.policy.revision, 3);
    expect(config.catalogs.processingModes, contains('local_full'));
    expect(config.catalogs.uploadScopes, contains('analysis'));
    expect(config.catalogs.redactionLevels, contains('strict'));
    expect(config.catalogs.blockReasons, contains('quota'));
  });

  test('publishes the policy with the draft JSON body', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'policy': <String, Object?>{
            'schema_version': 1,
            'enabled': true,
            'upload_allowed': true,
            'local_processing_only': false,
            'processing_mode': 'local_full',
            'upload_scope': 'thumbnails',
            'retention_days': 14,
            'redaction_level': 'standard',
            'require_child_consent': true,
            'revision': 4,
          },
        },
      },
    );
    final api = HttpImagePrivacyApi(client);

    final policy = await api.publishPolicy(
      const ImagePrivacyDraft(
        enabled: true,
        uploadAllowed: true,
        localProcessingOnly: false,
        processingMode: 'local_full',
        uploadScope: 'thumbnails',
        retentionDays: 14,
        redactionLevel: 'standard',
        requireChildConsent: true,
      ),
    );

    expect(policy.uploadAllowed, isTrue);
    expect(policy.revision, 4);
    expect(client.lastPutPath, '/api/v1/image-privacy/policy');
    expect(client.lastPutBody, <String, Object?>{
      'enabled': true,
      'upload_allowed': true,
      'local_processing_only': false,
      'processing_mode': 'local_full',
      'upload_scope': 'thumbnails',
      'retention_days': 14,
      'redaction_level': 'standard',
      'require_child_consent': true,
    });
  });

  test('parses bounded device status rows', () async {
    final api = HttpImagePrivacyApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'devices': <Object?>[
              <String, Object?>{
                'device_id': 'device-1',
                'camera_supported': true,
                'local_processing': true,
                'upload_allowed': true,
                'processing_mode': 'local_full',
                'upload_scope': 'analysis',
                'redaction_level': 'strict',
                'processed_count': 12,
                'uploaded_count': 3,
                'blocked_count': 2,
                'last_block_reason': 'scope',
                'up_to_date': true,
              },
            ],
          },
        },
      ),
    );

    final devices = await api.fetchDevices();

    expect(devices, hasLength(1));
    expect(devices.single.cameraSupported, isTrue);
    expect(devices.single.blocked, isTrue);
    expect(devices.single.uploadedCount, 3);
    expect(devices.single.lastBlockReason, 'scope');
    expect(devices.single.upToDate, isTrue);
    expect(imageBlockReasonLabel('scope'), '超出上传范围');
  });

  test('parses a device capability from the device path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'supported': true,
          'local_processing': true,
          'upload_allowed': false,
          'processing_mode': 'metadata',
          'upload_scope': 'none',
          'redaction_level': 'strict',
          'schema_version': 1,
          'policy_revision': 5,
          'applied_revision': 4,
          'up_to_date': false,
        },
      },
    );
    final api = HttpImagePrivacyApi(client);

    final capability = await api.fetchCapability('device/1');

    expect(
      client.lastGetPath,
      '/api/v1/devices/device%2F1/image-privacy/capability',
    );
    expect(capability.supported, isTrue);
    expect(capability.policyRevision, 5);
    expect(capability.appliedRevision, 4);
    expect(capability.upToDate, isFalse);
  });

  test('falls back to safe catalogs and labels', () {
    final catalogs = ImagePrivacyCatalogs.fromJson(<String, Object?>{
      'processing_modes': <Object?>[],
      'upload_scopes': 'bad',
    });

    expect(catalogs.processingModes, contains('off'));
    expect(catalogs.uploadScopes, contains('none'));
    expect(imageProcessingModeLabel('unexpected'), 'unexpected');
    expect(imageUploadScopeLabel('unexpected'), 'unexpected');
    expect(imageRedactionLevelLabel('unexpected'), 'unexpected');
    expect(imageBlockReasonLabel('unexpected'), '未知');
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  String? lastGetPath;
  String? lastPutPath;
  Object? lastPutBody;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastGetPath = path;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async =>
      response;

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    lastPutPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async => response;

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
