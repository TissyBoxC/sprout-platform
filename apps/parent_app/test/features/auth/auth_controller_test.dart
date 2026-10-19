import 'dart:convert';
import 'dart:io';
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
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/features/content_management/application/content_favorites_controller.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/application/content_library_store.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/data/family_api.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';
import 'package:parent_app/providers.dart';

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

  test(
    'logout invalidates family-scoped state before the next sign-in',
    () async {
      final secureStore = _MemorySecureStore({
        SessionStoreKeys.accessToken: 'expired',
        SessionStoreKeys.refreshToken: 'refresh',
      });
      final familyApi = _CountingFamilyApi();
      final container = _container(
        secureStore: secureStore,
        adapter: _RefreshAdapter(),
        authApi: _AuthApiStub(),
        familyApi: familyApi,
      );
      addTearDown(container.dispose);

      await container.read(authControllerProvider.future);
      await container.read(familyControllerProvider.future);
      expect(familyApi.calls, 1);

      await container.read(authControllerProvider.notifier).logout();
      await Future<void>.delayed(Duration.zero);
      await container.read(familyControllerProvider.future);

      expect(familyApi.calls, 2);
    },
  );

  test(
    'logout keeps user-domain providers safe when they are used again',
    () async {
      final secureStore = _MemorySecureStore({
        SessionStoreKeys.accessToken: 'expired',
        SessionStoreKeys.refreshToken: 'refresh',
      });
      final familyApi = _CountingFamilyApi();
      final deviceApi = _CountingDeviceApi();
      final container = _container(
        secureStore: secureStore,
        adapter: _RefreshAdapter(),
        authApi: _AuthApiStub(),
        familyApi: familyApi,
        deviceApi: deviceApi,
      );
      addTearDown(container.dispose);

      await container.read(authControllerProvider.future);
      await container.read(familyControllerProvider.future);
      await container.read(deviceBindingControllerProvider.future);

      await container.read(authControllerProvider.notifier).logout();
      await container.read(familyControllerProvider.future);
      final deviceState = await container.read(
        deviceBindingControllerProvider.future,
      );

      expect(deviceState.bindings, isEmpty);
      expect(familyApi.calls, 2);
      expect(deviceApi.listCalls, 2);
    },
  );

  test(
    'logout forces content and favorites to reload for the next account',
    () async {
      final supportDirectory = await Directory.systemTemp.createTemp(
        'sprout-session-content-test-',
      );
      addTearDown(() async {
        if (await supportDirectory.exists()) {
          await supportDirectory.delete(recursive: true);
        }
      });
      final secureStore = _MemorySecureStore({
        SessionStoreKeys.accessToken: 'expired',
        SessionStoreKeys.refreshToken: 'refresh',
      });
      final contentApi = _CountingContentApi();
      final contentStore = ContentLibraryStore(
        supportDirectory: () async => supportDirectory,
        temporaryDirectory: () async => supportDirectory,
      );
      final container = _container(
        secureStore: secureStore,
        adapter: _RefreshAdapter(),
        authApi: _AuthApiStub(),
        contentApi: contentApi,
        contentStore: contentStore,
      );
      addTearDown(container.dispose);

      await container.read(authControllerProvider.future);
      await container.read(contentFavoritesControllerProvider.future);
      await container.read(contentLibraryControllerProvider.future);
      expect(contentApi.favoriteCalls, 1);
      expect(contentApi.catalogCalls, 1);

      await container.read(authControllerProvider.notifier).logout();
      await container.read(contentFavoritesControllerProvider.future);
      await container.read(contentLibraryControllerProvider.future);

      expect(contentApi.favoriteCalls, 2);
      expect(contentApi.catalogCalls, 2);
    },
  );
}

ProviderContainer _container({
  required _MemorySecureStore secureStore,
  required _RefreshAdapter adapter,
  required _AuthApiStub authApi,
  _CountingFamilyApi? familyApi,
  _CountingDeviceApi? deviceApi,
  _CountingContentApi? contentApi,
  ContentLibraryStore? contentStore,
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
      if (familyApi != null) familyApiProvider.overrideWithValue(familyApi),
      if (deviceApi != null)
        deviceBindingApiProvider.overrideWithValue(deviceApi),
      if (contentApi != null)
        contentManagementApiProvider.overrideWithValue(contentApi),
      if (contentStore != null)
        contentLibraryStoreProvider.overrideWithValue(contentStore),
    ],
  );
}

class _CountingContentApi implements ContentManagementApi {
  int catalogCalls = 0;
  int favoriteCalls = 0;

  @override
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  }) async {
    catalogCalls += 1;
    return const ContentCatalog(
      catalogRevision: 1,
      packages: [],
      withdrawnPackageIds: [],
    );
  }

  @override
  Future<List<ContentFavorite>> fetchFavorites() async {
    favoriteCalls += 1;
    return const [];
  }

  @override
  Future<void> addFavorite(String packageId) async {}

  @override
  Future<void> removeFavorite(String packageId) async {}

  @override
  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId) {
    throw UnimplementedError();
  }

  @override
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId}) async {
    return const [];
  }

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async => const [];

  @override
  Future<List<ContentRecommendation>> fetchRecommendations({
    ContentAgeTier? ageTier,
    int limit = 8,
    List<String> excludePackageIds = const [],
  }) async => const [];
}

class _CountingDeviceApi implements DeviceBindingApi {
  int listCalls = 0;

  @override
  Future<List<BoundDevice>> list() async {
    listCalls += 1;
    return const [];
  }

  @override
  Future<void> remove(String deviceId) async {}

  @override
  Future<BoundDevice> bind({
    required String token,
    required String deviceName,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<DeviceProvisioningTicket> requestProvisioningTicket(
    String deviceId,
    String deviceSessionToken,
  ) {
    throw UnimplementedError();
  }
}

class _CountingFamilyApi implements FamilyApi {
  _CountingFamilyApi();

  int calls = 0;

  @override
  Future<FamilyCapability> fetchCapability() async {
    calls += 1;
    return const FamilyCapability(
      familyId: 'family-1',
      ownerUserId: 'account-001',
      memberCount: 1,
      pendingInviteCount: 0,
      schemaVersion: '1.0.0',
    );
  }

  @override
  Future<List<FamilyMember>> fetchMembers() async => const [];

  @override
  Future<List<FamilyInvitation>> fetchInvitations() async => const [];

  @override
  Future<FamilyInvitation> createInvitation({
    required String email,
    required String role,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<FamilyMember> updateMemberRole(String userId, String role) {
    throw UnimplementedError();
  }

  @override
  Future<void> removeMember(String userId) {
    throw UnimplementedError();
  }

  @override
  Future<void> revokeInvitation(String invitationId) {
    throw UnimplementedError();
  }

  @override
  Future<List<FamilyInvitation>> fetchMyInvitations() async => const [];

  @override
  Future<FamilyMember> acceptInvitation({
    required String familyId,
    required String invitationId,
  }) {
    throw UnimplementedError();
  }
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
    String? currentResourceVersion,
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
