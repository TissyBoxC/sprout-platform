import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';

void main() {
  test(
    'registration sends phone-first guardian and child profile fields',
    () async {
      final client = _RecordingApiClient();
      final api = AuthApi(client);

      await api.register(
        phone: '13800138000',
        phoneVerificationCode: '000000',
        password: 'sprout123',
        guardianFamilyName: '林',
        childNickname: '小芽',
        childBirthday: '2021-06-01',
        guardianConsentVersion: '2026-01',
      );

      expect(client.path, '/api/v1/auth/register');
      expect(client.body, {
        'phone': '13800138000',
        'phone_verification_code': '000000',
        'password': 'sprout123',
        'guardian_family_name': '林',
        'child_nickname': '小芽',
        'child_birthday': '2021-06-01',
        'guardian_consent_version': '2026-01',
      });
    },
  );

  test('login sends the unified phone-or-email identifier', () async {
    final client = _RecordingApiClient();
    final api = AuthApi(client);

    await api.login(identifier: 'guardian@example.com', password: 'sprout123');

    expect(client.path, '/api/v1/auth/login');
    expect(client.body, {
      'identifier': 'guardian@example.com',
      'password': 'sprout123',
    });
  });

  test('binding an email calls the authenticated account endpoint', () async {
    final client = _RecordingApiClient();
    final api = AuthApi(client);

    final account = await api.bindEmail(
      email: 'guardian@example.com',
      password: 'sprout123',
    );

    expect(client.path, '/api/v1/auth/email');
    expect(client.body, {
      'email': 'guardian@example.com',
      'password': 'sprout123',
    });
    expect(account.email, 'guardian@example.com');
  });

  test('overview maps today usage and device totals', () async {
    final client = _RecordingApiClient();
    final api = AuthApi(client);

    final overview = await api.overview();

    expect(client.path, '/api/v1/auth/overview');
    expect(overview.todayConversationCount, 12);
    expect(overview.todaySpentUsd, 0.36);
    expect(overview.remainingBalanceUsd, 8.64);
    expect(overview.deviceCount, 2);
    expect(overview.onlineDeviceCount, 1);
  });

  test(
    'profile update sends only editable guardian and child fields',
    () async {
      final client = _RecordingApiClient();
      final api = AuthApi(client);

      await api.updateProfile(
        displayName: '林妈妈',
        guardianFamilyName: '林',
        childNickname: '小芽',
        childBirthday: '2021-06-01',
      );

      expect(client.path, '/api/v1/auth/profile');
      expect(client.body, {
        'display_name': '林妈妈',
        'guardian_family_name': '林',
        'child_nickname': '小芽',
        'child_birthday': '2021-06-01',
      });
    },
  );

  test('update check sends platform and current version', () async {
    final client = _RecordingApiClient();
    final api = AuthApi(client);

    final update = await api.appUpdate(
      platform: 'android',
      currentVersion: '1.0.0',
    );

    expect(client.path, '/api/v1/app/update');
    expect(client.queryParameters, {
      'platform': 'android',
      'current_version': '1.0.0',
    });
    expect(update?.isClientUpdate, isFalse);
    expect(update?.version, '1.0.1');
  });

  test('204 update check means the client is current', () async {
    final client = _RecordingApiClient(updateResponse: _noContentEnvelope());
    final api = AuthApi(client);

    final update = await api.appUpdate(
      platform: 'android',
      currentVersion: '1.0.0',
    );

    expect(update, isNull);
  });

  test('malformed 200 update check fails closed', () async {
    final client = _RecordingApiClient(updateResponse: const {'data': null});
    final api = AuthApi(client);

    await expectLater(
      api.appUpdate(platform: 'android', currentVersion: '1.0.0'),
      throwsA(isA<FormatException>()),
    );
  });
}

class _RecordingApiClient implements ApiClient {
  _RecordingApiClient({this.updateResponse});

  final Map<String, Object?>? updateResponse;
  String? path;
  Object? body;
  Map<String, String>? queryParameters;

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    this.path = path;
    this.body = body;
    return _successEnvelope();
  }

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    this.path = path;
    this.queryParameters = queryParameters;
    if (path == '/api/v1/auth/overview') {
      return {
        'data': {
          'today_conversation_count': 12,
          'today_spent_usd': 0.36,
          'remaining_balance_usd': 8.64,
          'balance_usd': 9.0,
          'device_count': 2,
          'online_device_count': 1,
        },
      };
    }
    if (path == '/api/v1/app/update') {
      return updateResponse ??
          {
            'data': {
              'kind': 'resource',
              'version': '1.0.1',
              'download_url': 'https://example.com/sprout-resource.zip',
              'sha256': 'abc123',
              'release_notes': '更新了新的故事和儿歌内容。',
              'is_mandatory': false,
              'min_supported_version': '0.9.0',
            },
          };
    }
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    this.path = path;
    this.body = body;
    return _successEnvelope(email: 'guardian@example.com');
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
}

Map<String, Object?> _successEnvelope({String email = ''}) {
  return {
    'data': {
      'access_token': 'access-token',
      'refresh_token': 'refresh-token',
      'expires_in': 900,
      'account': {
        'id': 'account-001',
        'email': email,
        'phone': '13800138000',
        'display_name': '林家长',
        'guardian_family_name': '林',
        'child_nickname': '小芽',
        'child_birthday': '2021-06-01',
        'role': 'parent',
        'status': 'active',
      },
      'ai_account': null,
    },
  };
}

Map<String, Object?> _noContentEnvelope() {
  return ApiClientNoContent.emptyNoContent;
}
