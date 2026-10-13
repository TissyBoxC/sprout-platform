import 'package:dio/dio.dart';

import '../config/app_config.dart';
import '../error/api_error_mapper.dart';
import '../error/app_exception.dart';
import '../storage/secure_store.dart';
import 'api_client.dart';

/// Owns persisted guardian credentials and serializes token rotation.
///
/// Startup recovery and ordinary authenticated requests must not refresh with
/// the same one-time token independently. Sharing this manager keeps the
/// access/refresh pair consistent across both paths.
class SessionManager {
  SessionManager({
    required SecureStore secureStore,
    required AppConfig appConfig,
    Dio? dio,
  }) : _sessionStore = SessionStore(secureStore),
       _appConfig = appConfig,
       _dio = dio;

  final SessionStore _sessionStore;
  final AppConfig _appConfig;
  final Dio? _dio;
  Future<AuthSession>? _pendingRefresh;

  Future<StoredSession?> read() => _sessionStore.read();

  Future<void> save(AuthSession session) {
    return _sessionStore.write(
      StoredSession(
        accessToken: session.accessToken,
        refreshToken: session.refreshToken,
      ),
    );
  }

  Future<void> clear() => _sessionStore.clear();

  /// Refreshes a token, reusing an in-flight refresh and any rotation already
  /// persisted by another caller.
  Future<AuthSession> refresh(String refreshToken) {
    final pending = _pendingRefresh;
    if (pending != null) {
      return pending;
    }

    final refresh = _performRefresh(refreshToken);
    _pendingRefresh = refresh;
    return refresh.whenComplete(() {
      if (identical(_pendingRefresh, refresh)) {
        _pendingRefresh = null;
      }
    });
  }

  /// Refreshes the currently persisted session, if one exists.
  Future<AuthSession> refreshCurrent() async {
    final session = await _sessionStore.read();
    final refreshToken = session?.refreshToken;
    if (refreshToken == null || refreshToken.isEmpty) {
      throw const AppException(
        kind: AppErrorKind.unauthenticated,
        message: '登录已过期，请重新登录',
        retryable: false,
      );
    }
    return refresh(refreshToken);
  }

  Future<AuthSession> _performRefresh(String refreshToken) async {
    final storedSession = await _sessionStore.read();
    if (storedSession != null &&
        storedSession.accessToken.isNotEmpty &&
        storedSession.refreshToken != refreshToken) {
      return AuthSession(
        accessToken: storedSession.accessToken,
        refreshToken: storedSession.refreshToken,
      );
    }

    try {
      final response = await (_dio ?? Dio()).post<Object?>(
        _refreshEndpoint.toString(),
        data: {'refresh_token': refreshToken},
        options: Options(extra: const {'skip_session_refresh': true}),
      );
      final session = _readRefreshPayload(response.data);
      await save(session);
      return session;
    } on Object catch (error) {
      final mapped = mapApiError(error);
      if (mapped.kind == AppErrorKind.unauthenticated) {
        await _sessionStore.clear();
      }
      throw mapped;
    }
  }

  Uri get _refreshEndpoint {
    final base = _appConfig.apiBaseUrl;
    final basePath = base.path.replaceFirst(RegExp(r'/$'), '');
    return base.replace(
      path: '$basePath/api/v1/auth/refresh',
      query: null,
      fragment: null,
    );
  }

  AuthSession _readRefreshPayload(Object? responseData) {
    if (responseData is! Map) {
      throw StateError('invalid refresh response');
    }
    final envelope = Map<String, Object?>.from(responseData);
    final payload = envelope['data'];
    if (payload is! Map) {
      throw StateError('invalid refresh payload');
    }
    final data = Map<String, Object?>.from(payload);
    final accessToken = data['access_token'];
    final nextRefreshToken = data['refresh_token'];
    if (accessToken is! String ||
        accessToken.isEmpty ||
        nextRefreshToken is! String ||
        nextRefreshToken.isEmpty) {
      throw StateError('invalid refresh tokens');
    }
    return AuthSession(
      accessToken: accessToken,
      refreshToken: nextRefreshToken,
    );
  }
}
