import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../../../core/network/session_manager.dart';
import '../../../core/providers.dart';
import '../data/auth_api.dart';

const guardianConsentVersion = '2026-01';

/// Immutable authentication state consumed by the router and pages.
class AuthState {
  const AuthState({
    required this.isLoading,
    this.account,
    this.aiAccount,
    this.errorMessage,
    this.canRetryRestore = false,
  });

  const AuthState.loading() : this(isLoading: true);

  const AuthState.signedOut({this.errorMessage})
    : isLoading = false,
      account = null,
      aiAccount = null,
      canRetryRestore = false;

  const AuthState.restoreFailed({this.errorMessage})
    : isLoading = false,
      account = null,
      aiAccount = null,
      canRetryRestore = true;

  final bool isLoading;
  final ParentAccount? account;
  final AiAccount? aiAccount;
  final String? errorMessage;
  final bool canRetryRestore;

  bool get isSignedIn => account != null;

  AuthState copyWith({
    bool? isLoading,
    ParentAccount? account,
    AiAccount? aiAccount,
    String? errorMessage,
    bool? canRetryRestore,
  }) {
    return AuthState(
      isLoading: isLoading ?? this.isLoading,
      account: account ?? this.account,
      aiAccount: aiAccount ?? this.aiAccount,
      errorMessage: errorMessage,
      canRetryRestore: canRetryRestore ?? this.canRetryRestore,
    );
  }
}

/// Owns session restoration, registration, login, and sign-out.
class AuthController extends AsyncNotifier<AuthState> {
  late final AuthApi _authApi;
  late final SessionManager _sessionManager;

  @override
  Future<AuthState> build() async {
    _authApi = ref.read(authApiProvider);
    _sessionManager = ref.read(sessionManagerProvider);
    final storedSession = await _sessionManager.read();
    if (storedSession == null || !storedSession.hasRefreshToken) {
      return const AuthState.signedOut();
    }
    return _restoreSession();
  }

  /// Retries startup recovery without discarding a valid local session.
  ///
  /// Transient network and server failures leave the stored refresh token in
  /// place. Only an explicit authentication rejection clears it.
  Future<AuthState> retryRestore() async {
    state = const AsyncLoading();
    final nextState = await AsyncValue.guard(() async {
      final storedSession = await _sessionManager.read();
      if (storedSession == null || !storedSession.hasRefreshToken) {
        return const AuthState.signedOut();
      }
      return _restoreSession();
    });
    state = nextState;
    return nextState.value ?? const AuthState.restoreFailed();
  }

  Future<AuthState> _restoreSession() async {
    try {
      await _sessionManager.refreshCurrent();
      final account = await _authApi.me();
      return AuthState(
        isLoading: false,
        account: account.account,
        aiAccount: account.aiAccount,
      );
    } on Object catch (error) {
      if (isSessionRejected(error)) {
        await _clearSession();
        return AuthState.signedOut(errorMessage: authErrorMessage(error));
      }
      return AuthState.restoreFailed(errorMessage: authErrorMessage(error));
    }
  }

  Future<bool> register({
    required String phone,
    required String phoneVerificationCode,
    required String password,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) async {
    state = const AsyncLoading();
    final nextState = await AsyncValue.guard(() async {
      final session = await _authApi.register(
        phone: phone,
        phoneVerificationCode: phoneVerificationCode,
        password: password,
        guardianFamilyName: guardianFamilyName,
        childNickname: childNickname,
        childBirthday: childBirthday,
        guardianConsentVersion: guardianConsentVersion,
      );
      await _saveSession(session);
      return AuthState(
        isLoading: false,
        account: session.account,
        aiAccount: session.aiAccount,
      );
    });
    state = nextState;
    return nextState.hasValue && nextState.value?.isSignedIn == true;
  }

  Future<bool> login({
    required String identifier,
    required String password,
  }) async {
    state = const AsyncLoading();
    final nextState = await AsyncValue.guard(() async {
      final session = await _authApi.login(
        identifier: identifier,
        password: password,
      );
      await _saveSession(session);
      return AuthState(
        isLoading: false,
        account: session.account,
        aiAccount: session.aiAccount,
      );
    });
    state = nextState;
    return nextState.hasValue && nextState.value?.isSignedIn == true;
  }

  Future<void> sendPhoneVerification({
    required String phone,
    String purpose = 'register',
  }) async {
    await _authApi.sendPhoneVerification(phone: phone, purpose: purpose);
  }

  Future<void> bindEmail({
    required String email,
    required String password,
  }) async {
    final current = state.value;
    final account = await _authApi.bindEmail(email: email, password: password);
    state = AsyncData(
      AuthState(
        isLoading: false,
        account: account,
        aiAccount: current?.aiAccount,
      ),
    );
  }

  Future<void> logout() async {
    state = const AsyncLoading();
    final storedSession = await _sessionManager.read();
    final refreshToken = storedSession?.refreshToken;
    if (refreshToken != null && refreshToken.isNotEmpty) {
      try {
        await _authApi.logout(refreshToken);
      } on Object {
        // A failed server revoke must not keep the device signed in.
      }
    }
    await _clearSession();
    state = const AsyncData(AuthState.signedOut());
  }

  Future<void> refreshAccount() async {
    final account = await _authApi.me();
    state = AsyncData(
      AuthState(
        isLoading: false,
        account: account.account,
        aiAccount: account.aiAccount,
      ),
    );
  }

  /// Retries AI account preparation without recreating the guardian account.
  Future<void> retryAIService() async {
    await refreshAccount();
  }

  Future<void> updateSelectedModels(List<String> selectedModels) async {
    final account = state.value;
    if (account == null || account.account == null) {
      return;
    }
    final updatedAIAccount = await _authApi.updateSelectedModels(
      selectedModels,
    );
    state = AsyncData(
      AuthState(
        isLoading: false,
        account: account.account,
        aiAccount: updatedAIAccount,
      ),
    );
  }

  /// Saves editable profile fields and refreshes the shared account state.
  Future<void> updateProfile({
    required String displayName,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) async {
    final current = state.value;
    if (current?.account == null) {
      return;
    }
    final account = await _authApi.updateProfile(
      displayName: displayName,
      guardianFamilyName: guardianFamilyName,
      childNickname: childNickname,
      childBirthday: childBirthday,
    );
    state = AsyncData(
      AuthState(
        isLoading: false,
        account: account,
        aiAccount: current?.aiAccount,
      ),
    );
  }

  Future<void> _saveSession(AuthResult session) async {
    await _sessionManager.save(
      AuthSession(
        accessToken: session.accessToken,
        refreshToken: session.refreshToken,
      ),
    );
  }

  Future<void> _clearSession() async {
    await _sessionManager.clear();
  }
}

final authApiProvider = Provider<AuthApi>((ref) {
  return AuthApi(ref.read(apiClientProvider));
});

final authControllerProvider = AsyncNotifierProvider<AuthController, AuthState>(
  AuthController.new,
);

/// Converts a state error into copy the interface can safely display.
String authErrorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '操作没有完成，请稍后重试';
}
