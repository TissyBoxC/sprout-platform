import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/settings/data/privacy_api.dart';

void main() {
  test('reads consent, deletion, export, and retention state', () async {
    final client = _RecordingApiClient(
      getResponses: {
        '/api/v1/privacy': {
          'data': {
            'consent': {
              'guardian_consent_version': '2026-01',
              'guardian_consented_at': '2026-10-01T08:00:00Z',
              'withdrawn_at': null,
              'status': 'active',
              'reconsent_path': '/authorization/restore',
            },
            'deletion': {
              'status': 'pending',
              'requested_at': '2026-10-07T08:00:00Z',
              'scheduled_for': '2026-10-14T08:00:00Z',
              'completed_at': null,
              'cancellable': true,
            },
            'export': {'available': true},
            'policy': {
              'audio_retention_days': 0,
              'image_retention_days': 0,
              'conversation_retention_days': 30,
            },
          },
        },
      },
    );

    final status = await PrivacyApi(client).status();

    expect(status.consent.isActive, isTrue);
    expect(status.consent.version, '2026-01');
    expect(status.consent.reconsentRoute, isNull);
    expect(status.deletion.isPending, isTrue);
    expect(status.deletion.cancellable, isTrue);
    expect(status.exportAvailable, isTrue);
    expect(status.policy.conversationRetentionDays, 30);
    expect(client.lastPath, '/api/v1/privacy');
  });

  test('keeps only safe in-app reconsent routes', () async {
    for (final entry in <String, String?>{
      'relative': 'authorization/restore',
      'external': 'https://example.com/restore',
      'protocol-relative': '//example.com/restore',
      'valid': '/authorization/restore?source=privacy',
    }.entries) {
      final client = _RecordingApiClient(
        getResponses: {
          '/api/v1/privacy': {
            'data': {
              'consent': {
                'version': '2026-01',
                'consented_at': '2026-10-01T08:00:00Z',
                'withdrawn_at': '2026-10-07T09:00:00Z',
                'status': 'withdrawn',
                'reconsent_path': entry.value,
              },
              'deletion': {'status': 'none'},
              'policy': {
                'audio_retention_days': 0,
                'image_retention_days': 0,
                'conversation_retention_days': 30,
              },
            },
          },
        },
      );

      final status = await PrivacyApi(client).status();

      expect(
        status.consent.reconsentRoute,
        entry.key == 'valid' ? '/authorization/restore?source=privacy' : isNull,
        reason: entry.key,
      );
    }
  });

  test('sends the exact confirmations required by the server', () async {
    final client = _RecordingApiClient(
      postResponses: {
        '/api/v1/privacy/deletion': {
          'data': {
            'deletion': {
              'status': 'pending',
              'requested_at': '2026-10-07T08:00:00Z',
              'scheduled_for': '2026-10-14T08:00:00Z',
              'completed_at': null,
              'cancellable': true,
            },
          },
        },
        '/api/v1/privacy/consent/withdraw': {
          'data': {
            'consent': {
              'guardian_consent_version': '2026-01',
              'guardian_consented_at': '2026-10-01T08:00:00Z',
              'withdrawn_at': '2026-10-07T09:00:00Z',
              'status': 'withdrawn',
              'reconsent_path': '/authorization/restore',
            },
          },
        },
      },
      deleteResponses: {
        '/api/v1/privacy/deletion': {
          'data': {
            'deletion': {
              'status': 'none',
              'requested_at': null,
              'scheduled_for': null,
              'completed_at': null,
              'cancellable': false,
            },
          },
        },
      },
    );
    final api = PrivacyApi(client);

    await api.requestDeletion();
    await api.cancelDeletion();
    await api.withdrawConsent();

    expect(client.postBodies['/api/v1/privacy/deletion'], {
      'confirmation': 'DELETE',
    });
    expect(client.postBodies['/api/v1/privacy/consent/withdraw'], {
      'confirmation': 'WITHDRAW',
    });
    expect(client.deletedPaths, contains('/api/v1/privacy/deletion'));
  });

  test(
    'returns the export payload without inventing sensitive fields',
    () async {
      final client = _RecordingApiClient(
        getResponses: {
          '/api/v1/privacy/export': {
            'data': {
              'account': {'phone': '13800138000', 'display_name': '林家长'},
              'children': <Object?>[],
              'devices': <Object?>[],
              'usage_summary': {'conversation_count': 3},
              'consent_history': <Object?>[],
            },
          },
        },
      );

      final payload = await PrivacyApi(client).export();

      expect(payload.containsKey('account'), isTrue);
      expect(payload.containsKey('children'), isTrue);
      expect(payload.containsKey('devices'), isTrue);
      expect(payload.containsKey('usage_summary'), isTrue);
      expect(payload.containsKey('consent_history'), isTrue);
      expect(payload.toString(), isNot(contains('password')));
      expect(payload.toString(), isNot(contains('access_token')));
    },
  );
}

class _RecordingApiClient implements ApiClient {
  _RecordingApiClient({
    this.getResponses = const {},
    this.postResponses = const {},
    this.deleteResponses = const {},
  });

  final Map<String, Map<String, Object?>> getResponses;
  final Map<String, Map<String, Object?>> postResponses;
  final Map<String, Map<String, Object?>> deleteResponses;
  final Map<String, Object?> postBodies = {};
  final List<String> deletedPaths = [];
  String? lastPath;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastPath = path;
    return getResponses[path] ?? const {};
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    lastPath = path;
    postBodies[path] = body;
    return postResponses[path] ?? const {};
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    lastPath = path;
    deletedPaths.add(path);
    return deleteResponses[path] ?? const {};
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
  Future<Map<String, Object?>> put(String path, {Object? body}) {
    throw UnimplementedError();
  }
}
