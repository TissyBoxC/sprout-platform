import 'dart:async';

import 'package:dio/dio.dart';

import '../config/app_config.dart';
import '../error/api_error_mapper.dart';
import '../error/app_exception.dart';
import '../storage/secure_store.dart';

/// HTTP boundary used by feature data sources.
abstract interface class ApiClient {
  /// Internal key used only to identify a 204 response at the data boundary.
  static const noContentMarker = r'$no_content';

  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  });

  Future<Map<String, Object?>> post(String path, {Object? body});

  Future<Map<String, Object?>> put(String path, {Object? body});

  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  });

  Future<Map<String, Object?>> delete(String path);
}

/// Marker returned for a successful HTTP 204 response with no JSON envelope.
extension ApiClientNoContent on ApiClient {
  static const emptyNoContent = <String, Object?>{
    ApiClient.noContentMarker: true,
  };

  static bool isEmptyNoContent(Map<String, Object?> response) =>
      response[ApiClient.noContentMarker] == true;
}

/// Configuration needed to construct the production API client.
class ApiClientConfig {
  const ApiClientConfig({required this.appConfig});

  final AppConfig appConfig;
}

/// Refreshes an access token from the current refresh token.
typedef RefreshSession = Future<AuthSession> Function(String refreshToken);

/// Short-lived access token plus renewable refresh token.
class AuthSession {
  const AuthSession({required this.accessToken, required this.refreshToken});

  final String accessToken;
  final String refreshToken;
}

/// Runs at most one session refresh and shares its result with concurrent
/// unauthorized requests.
class SessionRefreshGate {
  Future<T> run<T>(Future<T> Function() refresh) async {
    final pending = _pending;
    if (pending != null) {
      return (await pending) as T;
    }
    final current = refresh();
    _pending = current;
    try {
      return await current;
    } finally {
      if (identical(_pending, current)) {
        _pending = null;
      }
    }
  }

  Future<dynamic>? _pending;
}

/// Dio-backed API client with authentication refresh and error mapping.
class DioApiClient implements ApiClient {
  DioApiClient({
    required ApiClientConfig config,
    required SecureStore secureStore,
    required RefreshSession refreshSession,
    Dio? dio,
  }) : _secureStore = secureStore,
       _refreshSession = refreshSession,
       _dio =
           dio ??
           Dio(
             BaseOptions(
               baseUrl: config.appConfig.apiBaseUrl.toString(),
               connectTimeout: config.appConfig.requestTimeout,
               receiveTimeout: config.appConfig.requestTimeout,
               sendTimeout: config.appConfig.requestTimeout,
               headers: const {'Content-Type': 'application/json'},
             ),
           ) {
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: _attachAccessToken,
        onError: _refreshAfterUnauthorized,
      ),
    );
  }

  static const accessTokenKey = SessionStoreKeys.accessToken;
  static const refreshTokenKey = SessionStoreKeys.refreshToken;
  static const _skipSessionRefreshExtra = 'skip_session_refresh';

  final Dio _dio;
  final SecureStore _secureStore;
  late final SessionStore _sessionStore = SessionStore(_secureStore);
  final RefreshSession _refreshSession;
  final _refreshGate = SessionRefreshGate();

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    try {
      final response = await _dio.get<Object?>(
        path,
        queryParameters: queryParameters,
      );
      // Some endpoints answer 204 with no body to mean "nothing to return",
      // which has no JSON envelope to decode. Surfacing that as a failure
      // would turn a normal result into an error.
      if (response.statusCode == 204) {
        return ApiClientNoContent.emptyNoContent;
      }
      return _requireResponseMap(response.data);
    } on Object catch (error) {
      throw mapApiError(error);
    }
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    try {
      final response = await _dio.post<Object?>(
        path,
        data: body,
        options: _publicAuthOptions(path),
      );
      return _requireResponseMap(response.data);
    } on Object catch (error) {
      throw mapApiError(error);
    }
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    try {
      final response = await _dio.put<Object?>(path, data: body);
      return _requireResponseMap(response.data);
    } on Object catch (error) {
      throw mapApiError(error);
    }
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) async {
    try {
      final response = await _dio.post<Object?>(
        path,
        data: body,
        options: Options(
          headers: {'Authorization': 'Bearer $bearerToken'},
          extra: const {'skip_auth': true},
        ),
      );
      return _requireResponseMap(response.data);
    } on Object catch (error) {
      throw mapApiError(error);
    }
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    try {
      final response = await _dio.delete<Object?>(path);
      return _requireResponseMap(response.data);
    } on Object catch (error) {
      throw mapApiError(error);
    }
  }

  Future<void> _attachAccessToken(
    RequestOptions request,
    RequestInterceptorHandler handler,
  ) async {
    if (request.extra['skip_auth'] == true) {
      handler.next(request);
      return;
    }
    final session = await _sessionStore.read();
    final accessToken = session?.accessToken;
    if (accessToken != null && accessToken.isNotEmpty) {
      request.headers['Authorization'] = 'Bearer $accessToken';
    }
    handler.next(request);
  }

  Future<void> _refreshAfterUnauthorized(
    DioException error,
    ErrorInterceptorHandler handler,
  ) async {
    final request = error.requestOptions;
    if (error.response?.statusCode != 401 ||
        request.extra['skip_auth'] == true ||
        request.extra[_skipSessionRefreshExtra] == true ||
        request.extra['auth_retried'] == true) {
      handler.next(error);
      return;
    }

    try {
      final outcome = await _refreshOnce();
      if (!outcome.didSucceed) {
        handler.next(error);
        return;
      }
      final accessToken = outcome.accessToken!;
      request.extra['auth_retried'] = true;
      request.headers['Authorization'] = 'Bearer $accessToken';
      final response = await _dio.fetch<Object?>(request);
      handler.resolve(response);
    } on Object {
      handler.next(error);
    }
  }

  Future<_RefreshOutcome> _refreshOnce() async {
    return _refreshGate.run(_performRefresh);
  }

  Future<_RefreshOutcome> _performRefresh() async {
    final settings = await _sessionStore.read();
    final refreshToken = settings?.refreshToken;
    if (refreshToken == null || refreshToken.isEmpty) {
      await _clearSession();
      return const _RefreshOutcome.failed();
    }

    try {
      final session = await _refreshSession(refreshToken);
      await _sessionStore.write(
        StoredSession(
          accessToken: session.accessToken,
          refreshToken: session.refreshToken,
        ),
      );
      return _RefreshOutcome.succeeded(session.accessToken);
    } on Object catch (error) {
      if (isSessionRejected(error)) {
        await _clearSession();
      }
      return const _RefreshOutcome.failed();
    }
  }

  Future<void> _clearSession() async {
    await _sessionStore.clear();
  }

  Map<String, Object?> _requireResponseMap(Object? data) {
    if (data is Map<String, Object?>) {
      return data;
    }
    if (data is Map) {
      return Map<String, Object?>.from(data);
    }
    throw const AppException(
      kind: AppErrorKind.unexpected,
      message: '操作没有完成，请稍后重试',
      retryable: true,
    );
  }

  // Public authentication endpoints use their 401 responses as domain
  // outcomes, not as an expired session signal. Refreshing here would replace
  // "invalid password" or "invalid code" with a misleading session error.
  Options _publicAuthOptions(String path) {
    if (path == '/api/v1/auth/login' ||
        path == '/api/v1/auth/register' ||
        path == '/api/v1/auth/refresh' ||
        path == '/api/v1/auth/phone-verification') {
      return Options(extra: const {_skipSessionRefreshExtra: true});
    }
    return Options();
  }
}

class _RefreshOutcome {
  const _RefreshOutcome._({required this.didSucceed, this.accessToken});

  const _RefreshOutcome.succeeded(String accessToken)
    : this._(didSucceed: true, accessToken: accessToken);

  const _RefreshOutcome.failed() : this._(didSucceed: false);

  final bool didSucceed;
  final String? accessToken;
}
