import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/config/app_config.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/core/storage/secure_store.dart';

void main() {
  test('refreshes once and retries the original request', () async {
    final secureStore = InMemorySecureStore();
    await secureStore.write(DioApiClient.accessTokenKey, 'expired');
    await secureStore.write(DioApiClient.refreshTokenKey, 'refresh');

    final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
    dio.httpClientAdapter = _SequenceAdapter();

    var refreshCount = 0;
    final client = DioApiClient(
      config: ApiClientConfig(
        appConfig: AppConfig(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          requestTimeout: const Duration(seconds: 1),
        ),
      ),
      secureStore: secureStore,
      refreshSession: (refreshToken) async {
        refreshCount += 1;
        return const AuthSession(
          accessToken: 'renewed',
          refreshToken: 'refresh-2',
        );
      },
      dio: dio,
    );

    final response = await client.get('/api/parent/v1/devices');

    expect(response, isNotEmpty);
    expect(refreshCount, 1);
    expect(await secureStore.read(DioApiClient.accessTokenKey), 'renewed');
    expect(await secureStore.read(DioApiClient.refreshTokenKey), 'refresh-2');
  });

  test('concurrent refresh callers share one refresh', () async {
    final gate = SessionRefreshGate();
    var refreshCount = 0;
    final refreshStarted = Completer<void>();
    final refreshCompleter = Completer<String>();

    Future<String> refresh() {
      refreshCount += 1;
      if (!refreshStarted.isCompleted) {
        refreshStarted.complete();
      }
      return refreshCompleter.future;
    }

    final firstResponse = gate.run(refresh);
    await refreshStarted.future;
    final secondResponse = gate.run(refresh);

    refreshCompleter.complete('renewed');

    final resolved = await Future.wait([firstResponse, secondResponse]);
    expect(resolved, ['renewed', 'renewed']);
    expect(refreshCount, 1);
  });

  test('refreshes a session stored as one compact value', () async {
    final secureStore = InMemorySessionSecureStore();
    await secureStore.writeSession(
      const StoredSession(accessToken: 'expired', refreshToken: 'refresh'),
    );

    final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
    dio.httpClientAdapter = _SequenceAdapter();

    var refreshCount = 0;
    final client = DioApiClient(
      config: ApiClientConfig(
        appConfig: AppConfig(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          requestTimeout: const Duration(seconds: 1),
        ),
      ),
      secureStore: secureStore,
      refreshSession: (refreshToken) async {
        refreshCount += 1;
        return const AuthSession(
          accessToken: 'renewed',
          refreshToken: 'refresh-2',
        );
      },
      dio: dio,
    );

    final response = await client.get('/api/parent/v1/devices');

    expect(response, isNotEmpty);
    expect(refreshCount, 1);
    expect((await secureStore.readSession())?.accessToken, 'renewed');
    expect((await secureStore.readSession())?.refreshToken, 'refresh-2');
  });

  test('a 204 response with no body yields an empty result', () async {
    final secureStore = InMemorySecureStore();
    final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
    dio.httpClientAdapter = _NoContentAdapter();

    final client = DioApiClient(
      config: ApiClientConfig(
        appConfig: AppConfig(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          requestTimeout: const Duration(seconds: 1),
        ),
      ),
      secureStore: secureStore,
      refreshSession: (refreshToken) async =>
          const AuthSession(accessToken: 'unused', refreshToken: 'unused'),
      dio: dio,
    );

    final response = await client.get('/api/v1/app/update');

    expect(response, isEmpty);
  });

  test('keeps the refresh token when refresh fails transiently', () async {
    final secureStore = InMemorySecureStore();
    await secureStore.write(DioApiClient.accessTokenKey, 'expired');
    await secureStore.write(DioApiClient.refreshTokenKey, 'refresh');

    final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
    dio.httpClientAdapter = _UnauthorizedAdapter();

    final client = DioApiClient(
      config: ApiClientConfig(
        appConfig: AppConfig(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          requestTimeout: const Duration(seconds: 1),
        ),
      ),
      secureStore: secureStore,
      refreshSession: (refreshToken) {
        throw const AppException(
          kind: AppErrorKind.network,
          message: '网络连接不稳定，请检查后重试',
          retryable: true,
        );
      },
      dio: dio,
    );

    await expectLater(
      client.get('/api/parent/v1/devices'),
      throwsA(isA<AppException>()),
    );
    expect(await secureStore.read(DioApiClient.refreshTokenKey), 'refresh');
  });

  test('clears the session when the refresh token is rejected', () async {
    final secureStore = InMemorySecureStore();
    await secureStore.write(DioApiClient.accessTokenKey, 'expired');
    await secureStore.write(DioApiClient.refreshTokenKey, 'refresh');

    final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
    dio.httpClientAdapter = _UnauthorizedAdapter();

    final client = DioApiClient(
      config: ApiClientConfig(
        appConfig: AppConfig(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          requestTimeout: const Duration(seconds: 1),
        ),
      ),
      secureStore: secureStore,
      refreshSession: (refreshToken) {
        throw const AppException(
          kind: AppErrorKind.unauthenticated,
          message: '登录已过期，请重新登录',
          retryable: false,
        );
      },
      dio: dio,
    );

    await expectLater(
      client.get('/api/parent/v1/devices'),
      throwsA(isA<AppException>()),
    );
    expect(await secureStore.read(DioApiClient.refreshTokenKey), isNull);
    expect(await secureStore.read(DioApiClient.accessTokenKey), isNull);
  });

  test('does not refresh a public login failure', () async {
    final secureStore = InMemorySecureStore();
    final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
    dio.httpClientAdapter = _InvalidCredentialsAdapter();

    var refreshCount = 0;
    final client = DioApiClient(
      config: ApiClientConfig(
        appConfig: AppConfig(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          requestTimeout: const Duration(seconds: 1),
        ),
      ),
      secureStore: secureStore,
      refreshSession: (refreshToken) async {
        refreshCount += 1;
        return const AuthSession(
          accessToken: 'renewed',
          refreshToken: 'refresh-2',
        );
      },
      dio: dio,
    );

    await expectLater(
      client.post(
        '/api/v1/auth/login',
        body: {'identifier': '13800138000', 'password': 'wrong-password'},
      ),
      throwsA(
        isA<AppException>().having(
          (error) => error.message,
          'message',
          '手机号、邮箱或密码不正确',
        ),
      ),
    );
    expect(refreshCount, 0);
  });
}

class _UnauthorizedAdapter implements HttpClientAdapter {
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return _unauthorizedResponse();
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody _unauthorizedResponse() {
  return ResponseBody.fromString(
    jsonEncode(<String, Object?>{
      'schema_version': '1.0.0',
      'request_id': 'request-unauthorized',
      'data': null,
      'error': <String, Object?>{
        'code': 'session_expired',
        'message': '登录已过期，请重新登录',
        'retryable': false,
      },
    }),
    401,
    headers: <String, List<String>>{
      Headers.contentTypeHeader: <String>[Headers.jsonContentType],
    },
  );
}

class InMemorySecureStore implements SecureStore {
  final Map<String, String> _values = {};

  @override
  Future<void> delete(String key) async {
    _values.remove(key);
  }

  @override
  Future<String?> read(String key) async {
    return _values[key];
  }

  @override
  Future<void> write(String key, String value) async {
    _values[key] = value;
  }
}

class InMemorySessionSecureStore implements SessionSecureStore {
  StoredSession? _session;

  @override
  Future<StoredSession?> readSession() async => _session;

  @override
  Future<void> writeSession(StoredSession session) async {
    _session = session;
  }

  @override
  Future<void> clearSession() async {
    _session = null;
  }

  @override
  Future<String?> read(String key) async => null;

  @override
  Future<void> write(String key, String value) async {}

  @override
  Future<void> delete(String key) async {}
}

class _SequenceAdapter implements HttpClientAdapter {
  var _requestCount = 0;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    _requestCount += 1;
    if (_requestCount == 1) {
      return ResponseBody.fromString(
        jsonEncode(<String, Object?>{
          'schema_version': '1.0.0',
          'request_id': 'request-001',
          'data': null,
          'error': <String, Object?>{
            'code': 'unauthenticated',
            'message': '登录已过期，请重新登录',
            'retryable': false,
          },
        }),
        401,
        headers: <String, List<String>>{
          Headers.contentTypeHeader: <String>[Headers.jsonContentType],
        },
      );
    }

    return ResponseBody.fromString(
      jsonEncode(<String, Object?>{'data': <String, Object?>{}}),
      200,
      headers: <String, List<String>>{
        Headers.contentTypeHeader: <String>[Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _InvalidCredentialsAdapter implements HttpClientAdapter {
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      jsonEncode(<String, Object?>{
        'schema_version': '1.0.0',
        'request_id': 'request-002',
        'data': null,
        'error': <String, Object?>{
          'code': 'invalid_credentials',
          'message': '手机号、邮箱或密码不正确',
          'retryable': false,
        },
      }),
      401,
      headers: <String, List<String>>{
        Headers.contentTypeHeader: <String>[Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _NoContentAdapter implements HttpClientAdapter {
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString('', 204);
  }

  @override
  void close({bool force = false}) {}
}
