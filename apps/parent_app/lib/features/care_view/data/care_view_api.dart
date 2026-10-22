import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../domain/care_view.dart';
import '../presentation/care_view_copy.dart';

/// Guardian-facing live view operations.
///
/// Every call is addressed through the signed-in family, so the platform can
/// check the guardian's role before a camera is even asked for. Pictures are
/// reached only through the stream location the platform returns; this layer
/// never returns a credential.
abstract interface class CareViewApi {
  /// Opens one look. Throws a [CareViewRefused] when the platform refuses.
  Future<CareView> start(String deviceId);

  Future<CareView> fetch(String viewId);

  Future<CareView> end(String viewId);

  Future<List<CareView>> recent({int limit});
}

/// Production implementation backed by the authenticated API client.
class HttpCareViewApi implements CareViewApi {
  const HttpCareViewApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String get _base => '/api/v1/families/${Uri.encodeComponent(_familyId)}';

  @override
  Future<CareView> start(String deviceId) {
    return _guarded(
      () => _apiClient.post(
        '$_base/devices/${Uri.encodeComponent(deviceId)}/care-view-sessions',
      ),
    );
  }

  @override
  Future<CareView> fetch(String viewId) {
    return _guarded(
      () => _apiClient.get(
        '$_base/care-view-sessions/${Uri.encodeComponent(viewId)}',
      ),
    );
  }

  @override
  Future<CareView> end(String viewId) {
    return _guarded(
      () => _apiClient.post(
        '$_base/care-view-sessions/${Uri.encodeComponent(viewId)}/end',
      ),
    );
  }

  @override
  Future<List<CareView>> recent({int limit = 20}) async {
    final response = await _apiClient.get(
      '$_base/care-view-sessions?limit=$limit',
    );
    final data = response['data'];
    final views = data is Map ? data['sessions'] : null;
    if (views is! List) {
      return const <CareView>[];
    }
    return views
        .whereType<Map>()
        .map((entry) => CareView.fromJson(Map<String, Object?>.from(entry)))
        .toList(growable: false);
  }

  /// Runs one call and replaces a coded refusal with a guardian-facing one.
  ///
  /// The platform's refusal code is the only durable part of the failure, so it
  /// is read and translated at this boundary. Nothing from the response body
  /// leaves this method.
  Future<CareView> _guarded(
    Future<Map<String, Object?>> Function() call,
  ) async {
    try {
      return _viewFrom(await call());
    } on AppException catch (error) {
      final refusal = _refusalFrom(error);
      if (refusal == null) {
        rethrow;
      }
      throw CareViewRefused(refusal);
    }
  }

  CareView _viewFrom(Map<String, Object?> response) {
    final data = response['data'];
    return CareView.fromJson(
      data is Map ? Map<String, Object?>.from(data) : <String, Object?>{},
    );
  }
}

CareViewRefusal? _refusalFrom(AppException error) {
  final code = readApiErrorCode(_bodyOf(error.cause));
  return switch (code) {
    'care_view_disabled' => CareViewRefusal.disabled,
    'care_view_consent_required' => CareViewRefusal.contentConsentRequired,
    'care_view_device_busy' => CareViewRefusal.deviceBusy,
    'care_view_state_changed' => CareViewRefusal.stateChanged,
    'care_view_rate_limited' => CareViewRefusal.rateLimited,
    'care_view_relay_unavailable' => CareViewRefusal.relayUnavailable,
    'care_view_device_not_found' => CareViewRefusal.deviceNotFound,
    'care_view_not_found' => CareViewRefusal.viewNotFound,
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
