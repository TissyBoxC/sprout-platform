import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/config/app_config.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/network/session_manager.dart';
import 'package:parent_app/core/storage/secure_store.dart';

void main() {
  test('writes a rotated token pair through secure storage', () async {
    final secureStore = InMemorySecureStore();
    final manager = SessionManager(
      secureStore: secureStore,
      appConfig: _config,
      dio: _adapterDio(
        _JsonAdapter(
          statusCode: 200,
          body: _refreshEnvelope(
            accessToken: 'access-2',
            refreshToken: 'refresh-2',
          ),
        ),
      ),
    );

    final session = await manager.refresh('refresh-1');

    expect(session.accessToken, 'access-2');
    expect(session.refreshToken, 'refresh-2');
    expect(await secureStore.read(SessionStoreKeys.accessToken), 'access-2');
    expect(await secureStore.read(SessionStoreKeys.refreshToken), 'refresh-2');
  });

  test('concurrent callers share one refresh request', () async {
    final secureStore = InMemorySecureStore();
    final adapter = _CountingAdapter(
      _refreshEnvelope(accessToken: 'access-2', refreshToken: 'refresh-2'),
    );
    final manager = SessionManager(
      secureStore: secureStore,
      appConfig: _config,
      dio: _adapterDio(adapter),
    );

    final sessions = await Future.wait([
      manager.refresh('refresh-1'),
      manager.refresh('refresh-1'),
    ]);

    expect(adapter.requestCount, 1);
    expect(
      sessions.every((session) => session.accessToken == 'access-2'),
      isTrue,
    );
  });

  test('reuses a token pair already rotated by another caller', () async {
    final secureStore = InMemorySecureStore();
    await secureStore.write(SessionStoreKeys.accessToken, 'access-2');
    await secureStore.write(SessionStoreKeys.refreshToken, 'refresh-2');
    final adapter = _CountingAdapter(
      _refreshEnvelope(accessToken: 'access-3', refreshToken: 'refresh-3'),
    );
    final manager = SessionManager(
      secureStore: secureStore,
      appConfig: _config,
      dio: _adapterDio(adapter),
    );

    final session = await manager.refresh('refresh-1');

    expect(session.accessToken, 'access-2');
    expect(session.refreshToken, 'refresh-2');
    expect(adapter.requestCount, 0);
  });

  test('keeps credentials when refresh times out', () async {
    final secureStore = InMemorySecureStore();
    await secureStore.write(SessionStoreKeys.accessToken, 'access-1');
    await secureStore.write(SessionStoreKeys.refreshToken, 'refresh-1');
    final manager = SessionManager(
      secureStore: secureStore,
      appConfig: _config,
      dio: _adapterDio(
        const _DioFailureAdapter(
          DioExceptionType.connectionTimeout,
          'temporary network failure',
        ),
      ),
    );

    await expectLater(
      manager.refresh('refresh-1'),
      throwsA(
        isA<AppException>().having(
          (error) => error.kind,
          'kind',
          AppErrorKind.network,
        ),
      ),
    );
    expect(await secureStore.read(SessionStoreKeys.accessToken), 'access-1');
    expect(await secureStore.read(SessionStoreKeys.refreshToken), 'refresh-1');
  });

  test('clears credentials when the server rejects refresh', () async {
    final secureStore = InMemorySecureStore();
    await secureStore.write(SessionStoreKeys.accessToken, 'access-1');
    await secureStore.write(SessionStoreKeys.refreshToken, 'refresh-1');
    final manager = SessionManager(
      secureStore: secureStore,
      appConfig: _config,
      dio: _adapterDio(
        const _JsonAdapter(
          statusCode: 401,
          body: <String, Object?>{
            'error': <String, Object?>{
              'code': 'session_expired',
              'message': '登录已过期，请重新登录',
              'retryable': false,
            },
          },
        ),
      ),
    );

    await expectLater(
      manager.refresh('refresh-1'),
      throwsA(
        isA<AppException>().having(
          (error) => error.kind,
          'kind',
          AppErrorKind.unauthenticated,
        ),
      ),
    );
    expect(await secureStore.read(SessionStoreKeys.accessToken), isNull);
    expect(await secureStore.read(SessionStoreKeys.refreshToken), isNull);
  });

  test('migrates legacy split credentials on first read', () async {
    final secureStore = InMemorySecureStore(<String, String>{
      SessionStoreKeys.accessToken: 'legacy-access',
      SessionStoreKeys.refreshToken: 'legacy-refresh',
    });
    final manager = SessionManager(
      secureStore: secureStore,
      appConfig: _config,
    );

    final session = await manager.read();

    expect(session?.accessToken, 'legacy-access');
    expect(session?.refreshToken, 'legacy-refresh');
  });
}

final _config = AppConfig(
  apiBaseUrl: Uri.parse('https://api.example.test'),
  requestTimeout: const Duration(seconds: 1),
);

Dio _adapterDio(HttpClientAdapter adapter) {
  final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
  dio.httpClientAdapter = adapter;
  return dio;
}

Map<String, Object?> _refreshEnvelope({
  required String accessToken,
  required String refreshToken,
}) {
  return <String, Object?>{
    'data': <String, Object?>{
      'access_token': accessToken,
      'refresh_token': refreshToken,
    },
  };
}

class InMemorySecureStore implements SecureStore {
  InMemorySecureStore([Map<String, String>? initialValues])
    : _values = <String, String>{...?initialValues};

  final Map<String, String> _values;

  @override
  Future<void> delete(String key) async {
    _values.remove(key);
  }

  @override
  Future<String?> read(String key) async => _values[key];

  @override
  Future<void> write(String key, String value) async {
    _values[key] = value;
  }
}

class _JsonAdapter implements HttpClientAdapter {
  const _JsonAdapter({required this.statusCode, required this.body});

  final int statusCode;
  final Object body;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      jsonEncode(body),
      statusCode,
      headers: <String, List<String>>{
        Headers.contentTypeHeader: <String>[Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _CountingAdapter extends _JsonAdapter {
  _CountingAdapter(Object body) : super(statusCode: 200, body: body);

  var requestCount = 0;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) {
    requestCount += 1;
    return super.fetch(options, requestStream, cancelFuture);
  }
}

class _DioFailureAdapter implements HttpClientAdapter {
  const _DioFailureAdapter(this.type, this.message);

  final DioExceptionType type;
  final String message;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) {
    throw DioException(requestOptions: options, type: type, error: message);
  }

  @override
  void close({bool force = false}) {}
}
