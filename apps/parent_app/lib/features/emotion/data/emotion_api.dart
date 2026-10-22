import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../domain/emotion.dart';
import '../presentation/emotion_copy.dart';

/// Guardian-facing 情绪感知 operations.
///
/// Every call is addressed through the signed-in family, so the platform can
/// check the guardian's role before any aggregate is read. There is no method
/// here for a single observation, because the platform deliberately has no
/// route that returns one: the guardian contract is a distribution over a
/// period.
abstract interface class EmotionApi {
  /// The family's configuration, or the platform's default when it never
  /// changed it. [EmotionSettings.isEnabled] is false in that case.
  Future<EmotionSettings> fetchSettings();

  /// Turns sensing on or off.
  ///
  /// [retentionDays] must be omitted rather than sent as zero when the
  /// guardian is not changing it: the platform reads zero as an invalid value
  /// and refuses the whole change.
  Future<EmotionSettings> updateSettings({
    required bool isEnabled,
    int? retentionDays,
  });

  /// The aggregate for one window.
  ///
  /// An empty [childId] asks for the whole family, which the platform accepts;
  /// a named child must belong to this family.
  Future<EmotionSummary> fetchSummary({String childId, int days});
}

/// Production implementation backed by the authenticated API client.
class HttpEmotionApi implements EmotionApi {
  const HttpEmotionApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String get _base => '/api/v1/families/${Uri.encodeComponent(_familyId)}';

  @override
  Future<EmotionSettings> fetchSettings() async {
    final response = await _guarded(
      () => _apiClient.get('$_base/emotion-settings'),
    );
    return EmotionSettings.fromJson(_dataOf(response));
  }

  @override
  Future<EmotionSettings> updateSettings({
    required bool isEnabled,
    int? retentionDays,
  }) async {
    final response = await _guarded(
      () => _apiClient.put(
        '$_base/emotion-settings',
        body: <String, Object?>{
          'is_enabled': isEnabled,
          // An omitted retention keeps the stored window. Sending zero would
          // be refused by the platform, so it is simply left out.
          if (retentionDays != null && retentionDays > 0)
            'retention_days': retentionDays,
        },
      ),
    );
    return EmotionSettings.fromJson(_dataOf(response));
  }

  @override
  Future<EmotionSummary> fetchSummary({
    String childId = '',
    int days = 7,
  }) async {
    final response = await _guarded(
      () => _apiClient.get(
        '$_base/emotion-summary',
        queryParameters: <String, String>{
          if (childId.isNotEmpty) 'child_id': childId,
          'days': '$days',
        },
      ),
    );
    return EmotionSummary.fromJson(_dataOf(response));
  }

  /// Runs one call and replaces a coded refusal with a guardian-facing one.
  ///
  /// The platform's refusal code is the only durable part of the failure, so it
  /// is read and translated at this boundary. Nothing from the response body
  /// leaves this method.
  Future<T> _guarded<T>(Future<T> Function() call) async {
    try {
      return await call();
    } on AppException catch (error) {
      final refusal = _refusalFrom(error);
      if (refusal == null) {
        rethrow;
      }
      throw EmotionRefused(refusal);
    }
  }

  Map<String, Object?> _dataOf(Map<String, Object?> response) {
    final data = response['data'];
    return data is Map ? Map<String, Object?>.from(data) : <String, Object?>{};
  }
}

EmotionRefusal? _refusalFrom(AppException error) {
  return switch (readApiErrorCode(_bodyOf(error.cause))) {
    'emotion_disabled' => EmotionRefusal.disabled,
    'emotion_invalid_retention' => EmotionRefusal.invalidRetention,
    'emotion_invalid_days' => EmotionRefusal.invalidDays,
    'emotion_child_not_bound' => EmotionRefusal.childNotFound,
    'emotion_summary_unavailable' => EmotionRefusal.unavailable,
    // Every remaining code describes a payload the guardian cannot fix, so it
    // falls through to the transport's sentence rather than a misleading hint.
    _ => null,
  };
}

/// Extracts the error body the transport attached to a mapped failure.
Object? _bodyOf(Object? cause) {
  if (cause == null) {
    return null;
  }
  final dynamic transportError = cause;
  try {
    return transportError.response?.data;
  } on Object {
    return null;
  }
}
