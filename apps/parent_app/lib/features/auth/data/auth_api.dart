import '../../../core/network/api_client.dart';

/// Authentication fields returned after register, login, and refresh.
class AuthResult {
  const AuthResult({
    required this.accessToken,
    required this.refreshToken,
    required this.account,
    this.aiAccount,
  });

  final String accessToken;
  final String refreshToken;
  final ParentAccount account;
  final AiAccount? aiAccount;

  factory AuthResult.fromResponse(Map<String, Object?> response) {
    final accountData = _requiredMap(response['account'], 'account');
    return AuthResult(
      accessToken: _requiredString(response['access_token'], 'access_token'),
      refreshToken: _requiredString(response['refresh_token'], 'refresh_token'),
      account: ParentAccount.fromJson(accountData),
      aiAccount: AiAccount.fromNullableJson(response['ai_account']),
    );
  }
}

/// Daily usage and device summary shown on the guardian home screen.
class ParentOverview {
  const ParentOverview({
    required this.todayConversationCount,
    required this.todaySpentUsd,
    required this.remainingBalanceUsd,
    required this.balanceUsd,
    required this.deviceCount,
    required this.onlineDeviceCount,
  });

  final int todayConversationCount;
  final double todaySpentUsd;
  final double remainingBalanceUsd;
  final double balanceUsd;
  final int deviceCount;
  final int onlineDeviceCount;

  factory ParentOverview.fromJson(Map<String, Object?> json) {
    return ParentOverview(
      todayConversationCount: _requiredInt(
        json['today_conversation_count'],
        'today_conversation_count',
      ),
      todaySpentUsd: _requiredDouble(
        json['today_spent_usd'],
        'today_spent_usd',
      ),
      remainingBalanceUsd: _requiredDouble(
        json['remaining_balance_usd'],
        'remaining_balance_usd',
      ),
      balanceUsd: _requiredDouble(json['balance_usd'], 'balance_usd'),
      deviceCount: _requiredInt(json['device_count'], 'device_count'),
      onlineDeviceCount: _requiredInt(
        json['online_device_count'],
        'online_device_count',
      ),
    );
  }
}

/// Available application update returned by the platform release service.
class AppUpdateInfo {
  const AppUpdateInfo({
    required this.kind,
    required this.version,
    required this.downloadUrl,
    required this.sha256,
    required this.releaseNotes,
    required this.isMandatory,
    required this.minSupportedVersion,
  });

  final String kind;
  final String version;
  final String downloadUrl;
  final String sha256;
  final String releaseNotes;
  final bool isMandatory;
  final String minSupportedVersion;

  bool get isClientUpdate => kind == 'client';
  bool get isResourceUpdate => kind == 'resource';

  factory AppUpdateInfo.fromJson(Map<String, Object?> json) {
    return AppUpdateInfo(
      kind: _requiredString(json['kind'], 'kind'),
      version: _requiredString(json['version'], 'version'),
      downloadUrl: _requiredString(json['download_url'], 'download_url'),
      sha256: _optionalString(json['sha256']),
      releaseNotes: _optionalString(json['release_notes']),
      isMandatory: json['is_mandatory'] == true,
      minSupportedVersion: _optionalString(json['min_supported_version']),
    );
  }
}

/// Parent identity safe to display in the application.
class ParentAccount {
  const ParentAccount({
    required this.id,
    required this.email,
    required this.phone,
    required this.displayName,
    required this.guardianFamilyName,
    required this.childNickname,
    required this.childBirthday,
    required this.role,
    required this.status,
  });

  final String id;
  final String email;
  final String phone;
  final String displayName;
  final String guardianFamilyName;
  final String childNickname;
  final String childBirthday;
  final String role;
  final String status;

  factory ParentAccount.fromJson(Map<String, Object?> json) {
    return ParentAccount(
      id: _requiredString(json['id'], 'id'),
      email: _optionalString(json['email']),
      phone: _requiredString(json['phone'], 'phone'),
      displayName: _requiredString(json['display_name'], 'display_name'),
      guardianFamilyName: _optionalString(json['guardian_family_name']),
      childNickname: _optionalString(json['child_nickname']),
      childBirthday: _optionalString(json['child_birthday']),
      role: _requiredString(json['role'], 'role'),
      status: _requiredString(json['status'], 'status'),
    );
  }
}

/// Parent-safe AI account projection. It never contains a provider API key.
class AiAccount {
  const AiAccount({
    required this.status,
    required this.balanceUsd,
    required this.concurrencyLimit,
    required this.availableModels,
    required this.selectedModels,
    required this.allowedModels,
    required this.providerReady,
  });

  final String status;
  final double balanceUsd;
  final int concurrencyLimit;

  /// Platform-approved models a guardian may choose from.
  final List<String> availableModels;

  /// Guardian's explicit selection. Empty means all available models.
  final List<String> selectedModels;

  /// Effective allowlist currently applied to the AI service.
  final List<String> allowedModels;
  final bool providerReady;

  static AiAccount? fromNullableJson(Object? value) {
    if (value is! Map) {
      return null;
    }
    return AiAccount.fromJson(Map<String, Object?>.from(value));
  }

  factory AiAccount.fromJson(Map<String, Object?> json) {
    return AiAccount(
      status: _requiredString(json['status'], 'status'),
      balanceUsd: _requiredDouble(json['balance_usd'], 'balance_usd'),
      concurrencyLimit: _requiredInt(
        json['concurrency_limit'],
        'concurrency_limit',
      ),
      availableModels: _stringList(json['available_models']),
      selectedModels: _stringList(json['selected_models']),
      allowedModels: _stringList(json['allowed_models']),
      providerReady: json['provider_ready'] == true,
    );
  }
}

/// Authentication API used by the application state.
class AuthApi {
  const AuthApi(this._apiClient);

  final ApiClient _apiClient;

  Future<AuthResult> register({
    required String phone,
    required String phoneVerificationCode,
    required String password,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
    required String guardianConsentVersion,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/auth/register',
      body: {
        'phone': phone,
        'phone_verification_code': phoneVerificationCode,
        'password': password,
        'guardian_family_name': guardianFamilyName,
        'child_nickname': childNickname,
        'child_birthday': childBirthday,
        'guardian_consent_version': guardianConsentVersion,
      },
    );
    return AuthResult.fromResponse(response['data'] as Map<String, Object?>);
  }

  Future<void> sendPhoneVerification({
    required String phone,
    required String purpose,
  }) async {
    await _apiClient.post(
      '/api/v1/auth/phone-verification',
      body: {'phone': phone, 'purpose': purpose},
    );
  }

  Future<AuthResult> login({
    required String identifier,
    required String password,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/auth/login',
      body: {'identifier': identifier, 'password': password},
    );
    return AuthResult.fromResponse(response['data'] as Map<String, Object?>);
  }

  Future<AuthResult> refresh(String refreshToken) async {
    final response = await _apiClient.post(
      '/api/v1/auth/refresh',
      body: {'refresh_token': refreshToken},
    );
    final data = response['data'] as Map<String, Object?>;
    return AuthResult(
      accessToken: _requiredString(data['access_token'], 'access_token'),
      refreshToken: _requiredString(data['refresh_token'], 'refresh_token'),
      account: const ParentAccount(
        id: '',
        email: '',
        phone: '',
        displayName: '',
        guardianFamilyName: '',
        childNickname: '',
        childBirthday: '',
        role: 'parent',
        status: 'active',
      ),
    );
  }

  Future<void> logout(String refreshToken) async {
    await _apiClient.post(
      '/api/v1/auth/logout',
      body: {'refresh_token': refreshToken},
    );
  }

  Future<AuthResult> me() async {
    final response = await _apiClient.get('/api/v1/auth/me');
    final data = response['data'] as Map<String, Object?>;
    return AuthResult(
      accessToken: '',
      refreshToken: '',
      account: ParentAccount.fromJson(_requiredMap(data['account'], 'account')),
      aiAccount: AiAccount.fromNullableJson(data['ai_account']),
    );
  }

  Future<ParentAccount> bindEmail({
    required String email,
    required String password,
  }) async {
    final response = await _apiClient.put(
      '/api/v1/auth/email',
      body: {'email': email, 'password': password},
    );
    final data = response['data'] as Map<String, Object?>;
    return ParentAccount.fromJson(_requiredMap(data['account'], 'account'));
  }

  Future<AiAccount> updateSelectedModels(List<String> selectedModels) async {
    final response = await _apiClient.put(
      '/api/v1/auth/ai-models',
      body: {'selected_models': selectedModels},
    );
    final data = response['data'] as Map<String, Object?>;
    return AiAccount.fromJson(_requiredMap(data['ai_account'], 'ai_account'));
  }

  /// Loads the small dashboard shown after a guardian signs in.
  Future<ParentOverview> overview() async {
    final response = await _apiClient.get('/api/v1/auth/overview');
    final data = response['data'] as Map<String, Object?>;
    return ParentOverview.fromJson(data);
  }

  /// Updates editable guardian and child profile fields.
  Future<ParentAccount> updateProfile({
    required String displayName,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) async {
    final response = await _apiClient.put(
      '/api/v1/auth/profile',
      body: {
        'display_name': displayName,
        'guardian_family_name': guardianFamilyName,
        'child_nickname': childNickname,
        'child_birthday': childBirthday,
      },
    );
    final data = response['data'] as Map<String, Object?>;
    return ParentAccount.fromJson(_requiredMap(data['account'], 'account'));
  }

  /// Returns the next available update, or null when the client is current.
  ///
  /// `current_resource_version` is forward-compatible: the current platform
  /// may ignore it, while the client still suppresses an already activated
  /// resource release locally.
  Future<AppUpdateInfo?> appUpdate({
    required String platform,
    required String currentVersion,
    String? currentResourceVersion,
  }) async {
    final resourceVersion = currentResourceVersion?.trim() ?? '';
    final response = await _apiClient.get(
      '/api/v1/app/update',
      queryParameters: {
        'platform': platform,
        'current_version': currentVersion,
        if (resourceVersion.isNotEmpty)
          'current_resource_version': resourceVersion,
      },
    );
    if (ApiClientNoContent.isEmptyNoContent(response)) {
      return null;
    }
    final data = response['data'];
    if (data is Map) {
      return AppUpdateInfo.fromJson(Map<String, Object?>.from(data));
    }
    // A 200 must carry an update envelope. Treating a missing or malformed
    // body as "current" would let a broken update service bypass a mandatory
    // release gate.
    throw const FormatException('missing app update data');
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}

String _requiredString(Object? value, String field) {
  if (value is String && value.isNotEmpty) {
    return value;
  }
  throw FormatException('missing $field');
}

String _optionalString(Object? value) {
  return value is String ? value : '';
}

double _requiredDouble(Object? value, String field) {
  if (value is num) {
    return value.toDouble();
  }
  throw FormatException('missing $field');
}

int _requiredInt(Object? value, String field) {
  if (value is num) {
    return value.toInt();
  }
  throw FormatException('missing $field');
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value.whereType<String>().toList(growable: false);
}
