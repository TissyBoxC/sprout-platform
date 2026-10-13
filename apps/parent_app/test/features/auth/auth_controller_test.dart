import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/config/app_config.dart';
import 'package:parent_app/core/network/session_manager.dart';
import 'package:parent_app/core/providers.dart';
import 'package:parent_app/core/storage/secure_store.dart';
import 'package:parent_app/features/auth/application/auth_controller.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';

void main() {
  test('restores a persisted refresh token on cold start', () async {
    final secureStore = _MemorySecureStore({
      SessionStoreKeys.accessToken: 'expired',
      SessionStoreKeys.refreshToken: 'refresh',
    });
    final adapter = _RefreshAdapter();
    final api = _AuthApiStub();
    final container = _container(
      secureStore: secureStore,
      adapter: adapter,
      authApi: api,
    );
    addTearDown(container.dispose);

    final state = await container.read(authControllerProvider.future);

    expect(state.isSignedIn, isTrue);
    expect(adapter.refreshCount, 1);
    expect(api.meCount, 1);
    expect(secureStore.values[SessionStoreKeys.accessToken], 'access-renewed');
    expect(
      secureStore.values[SessionStoreKeys.refreshToken],
      'refresh-renewed',
    );
  });

  test('keeps the session when startup refresh has a network error', () async {
    final secureStore = _MemorySecureStore({
      SessionStoreKeys.accessToken: 'expired',
      SessionStoreKeys.refreshToken: 'refresh',
    });
    final adapter = _RefreshAdapter(timeout: true);
    final api = _AuthApiStub();
    final container = _container(
      secureStore: secureStore,
      adapter: adapter,
      authApi: api,
    );
    addTearDown(container.dispose);

    final state = await container.read(authControllerProvider.future);

    expect(state.canRetryRestore, isTrue);
    expect(state.isSignedIn, isFalse);
    expect(state.errorMessage, '网络连接不稳定，请检查后重试');
    expect(secureStore.values[SessionStoreKeys.accessToken], 'expired');
    expect(secureStore.values[SessionStoreKeys.refreshToken], 'refresh');
  });

  test('clears the session when startup refresh is rejected', () async {
    final secureStore = _MemorySecureStore({
      SessionStoreKeys.accessToken: 'access',
      SessionStoreKeys.refreshToken: 'refresh',
    });
    final adapter = _RefreshAdapter(reject: true);
    final api = _AuthApiStub();
    final container = _container(
      secureStore: secureStore,
      adapter: adapter,
      authApi: api,
    );
    addTearDown(container.dispose);

    final state = await container.read(authControllerProvider.future);

    expect(state.isSignedIn, isFalse);
    expect(state.canRetryRestore, isFalse);
    expect(secureStore.values, isEmpty);
  });

  test('logout clears local session even when server logout fails', () async {
    final secureStore = _MemorySecureStore({
      SessionStoreKeys.accessToken: 'access',
      SessionStoreKeys.refreshToken: 'refresh',
    });
    final adapter = _RefreshAdapter();
    final api = _AuthApiStub(logoutError: StateError('network unavailable'));
    final container = _container(
      secureStore: secureStore,
      adapter: adapter,
      authApi: api,
    );
    addTearDown(container.dispose);

    await container.read(authControllerProvider.future);
    await container.read(authControllerProvider.notifier).logout();

    expect(api.logoutCount, 1);
    expect(secureStore.values, isEmpty);
    expect(container.read(authControllerProvider).value?.isSignedIn, isFalse);
  });
}

ProviderContainer _container({
  required _MemorySecureStore secureStore,
  required _RefreshAdapter adapter,
  required _AuthApiStub authApi,
}) {
  final sessionManager = SessionManager(
    secureStore: secureStore,
    appConfig: _config,
    dio: _adapterDio(adapter),
  );
  return ProviderContainer(
    overrides: [
      secureStoreProvider.overrideWithValue(secureStore),
      appConfigProvider.overrideWithValue(_config),
      sessionManagerProvider.overrideWithValue(sessionManager),
      authApiProvider.overrideWithValue(authApi),
    ],
  );
}

Dio _adapterDio(HttpClientAdapter adapter) {
  final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
  dio.httpClientAdapter = adapter;
  return dio;
}

final _config = AppConfig(
  apiBaseUrl: Uri.parse('https://api.example.test'),
  requestTimeout: const Duration(seconds: 1),
);

class _MemorySecureStore implements SecureStore {
  _MemorySecureStore(this.values);

  final Map<String, String> values;

  @override
  Future<void> delete(String key) async {
    values.remove(key);
  }

  @override
  Future<String?> read(String key) async {
    return values[key];
  }

  @override
  Future<void> write(String key, String value) async {
    values[key] = value;
  }
}

class _AuthApiStub implements AuthApi {
  _AuthApiStub({this.logoutError});

  final Object? logoutError;
  int meCount = 0;
  int logoutCount = 0;

  @override
  Future<AuthResult> refresh(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> me() async {
    meCount += 1;
    return const AuthResult(
      accessToken: '',
      refreshToken: '',
      account: _account,
    );
  }

  @override
  Future<void> logout(String refreshToken) async {
    logoutCount += 1;
    final error = logoutError;
    if (error != null) {
      throw error;
    }
  }

  @override
  Future<AuthResult> login({
    required String identifier,
    required String password,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> register({
    required String phone,
    required String phoneVerificationCode,
    required String password,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
    required String guardianConsentVersion,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<void> sendPhoneVerification({
    required String phone,
    required String purpose,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<ParentAccount> bindEmail({
    required String email,
    required String password,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AiAccount> updateSelectedModels(List<String> selectedModels) {
    throw UnimplementedError();
  }

  @override
  Future<ParentOverview> overview() {
    throw UnimplementedError();
  }

  @override
  Future<ParentAccount> updateProfile({
    required String displayName,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AppUpdateInfo?> appUpdate({
    required String platform,
    required String currentVersion,
  }) {
    throw UnimplementedError();
  }
}

class _RefreshAdapter implements HttpClientAdapter {
  _RefreshAdapter({this.reject = false, this.timeout = false});

  final bool reject;
  final bool timeout;
  var refreshCount = 0;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    if (!options.path.endsWith('/api/v1/auth/refresh')) {
      throw StateError('unexpected request: ${options.path}');
    }
    refreshCount += 1;
    if (timeout) {
      throw DioException(
        requestOptions: options,
        type: DioExceptionType.connectionTimeout,
        error: 'temporary network failure',
      );
    }
    if (reject) {
      return _errorResponse();
    }
    return ResponseBody.fromString(
      jsonEncode(<String, Object?>{
        'data': <String, Object?>{
          'access_token': 'access-renewed',
          'refresh_token': 'refresh-renewed',
        },
      }),
      200,
      headers: <String, List<String>>{
        Headers.contentTypeHeader: <String>[Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody _errorResponse() {
  return ResponseBody.fromString(
    jsonEncode(<String, Object?>{
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

const _account = ParentAccount(
  id: 'account-001',
  email: 'guardian@example.com',
  phone: '13800138000',
  displayName: '林家长',
  guardianFamilyName: '林',
  childNickname: '小芽',
  childBirthday: '2021-06-01',
  role: 'parent',
  status: 'active',
);
