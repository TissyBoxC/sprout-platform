import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../../../core/network/session_manager.dart';
import '../../../core/providers.dart';
import '../../alarm_reminder/application/alarm_reminder_controller.dart';
import '../../bluetooth_audio/application/bluetooth_audio_controller.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../companionship/application/companionship_controller.dart';
import '../../connectivity/application/cellular_controller.dart';
import '../../content_management/application/content_favorites_controller.dart';
import '../../content_management/application/content_library_controller.dart';
import '../../content_management/application/content_play_history_controller.dart';
import '../../content_management/application/content_recommendation_controller.dart';
import '../../content_management/application/content_theme_pack_controller.dart';
import '../../device/application/device_binding_controller.dart';
import '../../display/application/display_controller.dart';
import '../../english/application/english_controller.dart';
import '../../family/application/family_controller.dart';
import '../../form_factor/application/form_factor_controller.dart';
import '../../image_privacy/application/image_privacy_controller.dart';
import '../../learning_visualization/application/learning_visualization_controller.dart';
import '../../local_command/application/local_command_controller.dart';
import '../../motion/application/motion_controller.dart';
import '../../multi_device/application/multi_device_controller.dart';
import '../../notification/application/notification_controller.dart';
import '../../offline_policy/application/offline_policy_controller.dart';
import '../../parent_policy/application/parent_policy_controller.dart';
import '../../playback/application/playback_controller.dart';
import '../../power/application/power_controller.dart';
import '../../screen/application/screen_controller.dart';
import '../../settings/application/privacy_controller.dart';
import '../../touch/application/touch_controller.dart';
import '../../usage_report/application/usage_report_controller.dart';
import '../../video_companion/application/video_companion_controller.dart';
import '../../vision/application/vision_controller.dart';
import '../../wearable/application/wearable_controller.dart';
import '../data/auth_api.dart';

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
  late AuthApi _authApi;
  late SessionManager _sessionManager;
  RegistrationPolicy? _registrationPolicy;

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
    // A persisted access token may still be valid. Probe it first so a
    // transient refresh failure cannot lock a signed-in guardian out of
    // cached devices, messages, and settings.
    try {
      final account = await _authApi.me();
      return _signedInState(account);
    } on Object catch (error) {
      if (!isSessionRejected(error)) {
        return AuthState.restoreFailed(errorMessage: authErrorMessage(error));
      }
      // Only an explicit session rejection authorizes refresh. Network and
      // server failures above must preserve the existing token pair.
      try {
        await _sessionManager.refreshCurrent();
        final account = await _authApi.me();
        return _signedInState(account);
      } on Object catch (refreshError) {
        if (isSessionRejected(refreshError)) {
          await _clearSession();
          _resetUserData();
          return AuthState.signedOut(
            errorMessage: authErrorMessage(refreshError),
          );
        }
        return AuthState.restoreFailed(
          errorMessage: authErrorMessage(refreshError),
        );
      }
    }
  }

  AuthState _signedInState(AuthResult account) {
    _resetUserData();
    return AuthState(
      isLoading: false,
      account: account.account,
      aiAccount: account.aiAccount,
    );
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
      final policy = await _loadRegistrationPolicy();
      final session = await _authApi.register(
        phone: phone,
        phoneVerificationCode: phoneVerificationCode,
        password: password,
        guardianFamilyName: guardianFamilyName,
        childNickname: childNickname,
        childBirthday: childBirthday,
        guardianConsentVersion: policy.guardianConsentVersion,
      );
      await _saveSession(session);
      _resetUserData();
      return AuthState(
        isLoading: false,
        account: session.account,
        aiAccount: session.aiAccount,
      );
    });
    state = nextState;
    return nextState.hasValue && nextState.value?.isSignedIn == true;
  }

  /// Loads the public agreement revision immediately before registration so a
  /// long-lived form cannot submit a stale consent version after an operator
  /// publishes a new agreement. A load failure blocks registration instead of
  /// signing with a client-side default.
  Future<RegistrationPolicy> _loadRegistrationPolicy() async {
    final policy = await _authApi.registrationPolicy();
    _registrationPolicy = policy;
    return policy;
  }

  RegistrationPolicy? get registrationPolicySnapshot => _registrationPolicy;

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
      _resetUserData();
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
    _resetUserData();
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

  /// Invalidates every family-scoped provider before exposing a new identity.
  ///
  /// These providers outlive pages because their state backs the persistent
  /// navigation shell. Clearing them here prevents a subsequent sign-in from
  /// rendering the previous guardian's family, devices, content or favorites.
  void _resetUserData() {
    ref.invalidate(alarmReminderControllerProvider);
    ref.invalidate(bluetoothAudioControllerProvider);
    ref.invalidate(childProfileControllerProvider);
    ref.invalidate(companionshipControllerProvider);
    ref.invalidate(cellularControllerProvider);
    ref.invalidate(cellularCapabilityProvider);
    ref.invalidate(contentFavoritesControllerProvider);
    ref.invalidate(contentLibraryControllerProvider);
    ref.invalidate(contentPlayHistoryControllerProvider);
    ref.invalidate(contentRecommendationControllerProvider);
    ref.invalidate(contentThemePackControllerProvider);
    ref.invalidate(deviceBindingControllerProvider);
    ref.invalidate(displayControllerProvider);
    ref.invalidate(displayCapabilityProvider);
    ref.invalidate(englishControllerProvider);
    ref.invalidate(familyControllerProvider);
    ref.invalidate(imagePrivacyControllerProvider);
    ref.invalidate(learningVisualizationControllerProvider);
    ref.invalidate(localCommandControllerProvider);
    ref.invalidate(motionControllerProvider);
    ref.invalidate(motionCapabilityProvider);
    ref.invalidate(multiDeviceControllerProvider);
    ref.invalidate(myInvitationsControllerProvider);
    ref.invalidate(notificationControllerProvider);
    ref.invalidate(offlinePolicyControllerProvider);
    ref.invalidate(parentPolicyControllerProvider);
    ref.invalidate(playbackControllerProvider);
    ref.invalidate(powerControllerProvider);
    ref.invalidate(powerCapabilityProvider);
    ref.invalidate(privacyControllerProvider);
    ref.invalidate(screenControllerProvider);
    ref.invalidate(touchControllerProvider);
    ref.invalidate(touchCapabilityProvider);
    ref.invalidate(usageReportControllerProvider);
    ref.invalidate(videoCompanionControllerProvider);
    ref.invalidate(videoCapabilityProvider);
    ref.invalidate(wearableControllerProvider);
    // Capability snapshots are account-scoped too, even though they are not
    // AsyncNotifier controllers.
    ref.invalidate(formFactorCapabilityProvider);
    ref.invalidate(visionControllerProvider);
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
