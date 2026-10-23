import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../domain/video_session.dart';
import '../presentation/video_session_copy.dart';

/// Guardian-facing 视频通话 operations.
///
/// The policy is one per family and the platform scopes every call to the
/// signed-in guardian's family, so the client never sends an owner id. Only
/// negotiation metadata travels here: no frame, clip, or recording has a route
/// to call.
abstract interface class VideoSessionApi {
  /// The family policy plus the quality catalog the platform serves with it.
  Future<VideoSessionSnapshot> fetchPolicy();

  /// Validates and stores a new family policy revision.
  Future<VideoSessionPolicy> publishPolicy(VideoSessionPolicy policy);

  /// Each family device's latest bounded call status.
  Future<List<VideoSessionDevice>> fetchDevices();
}

/// The policy and the catalog values that arrived with it.
///
/// The catalog is served rather than hard-coded so a quality band the platform
/// adds later is not silently missing from the screen.
class VideoSessionSnapshot {
  const VideoSessionSnapshot({required this.policy, required this.qualities});

  final VideoSessionPolicy policy;
  final List<VideoQualityFloor> qualities;

  factory VideoSessionSnapshot.fromJson(Map<String, Object?> json) {
    final policy = json['policy'];
    return VideoSessionSnapshot(
      policy: VideoSessionPolicy.fromJson(
        policy is Map ? Map<String, Object?>.from(policy) : const {},
      ),
      qualities: _qualitiesOf(json['qualities']),
    );
  }

  static List<VideoQualityFloor> _qualitiesOf(Object? value) {
    final raw = value is List ? value.whereType<String>() : const <String>[];
    final parsed = <VideoQualityFloor>[
      for (final item in raw)
        // `unknown` is the band a device reports before it measured anything;
        // it is not a floor a guardian can choose.
        if (item != 'unknown') VideoQualityFloor.fromWire(item),
    ];
    // A missing or unreadable catalog falls back to the documented bands, so
    // the screen still offers a choice instead of an empty control.
    return parsed.isEmpty ? VideoQualityFloor.values : parsed;
  }
}

/// Production implementation backed by the authenticated API client.
class HttpVideoSessionApi implements VideoSessionApi {
  const HttpVideoSessionApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<VideoSessionSnapshot> fetchPolicy() async {
    final response = await _guarded(
      () => _apiClient.get('/api/v1/video-session/policy'),
    );
    return VideoSessionSnapshot.fromJson(_dataOf(response));
  }

  @override
  Future<VideoSessionPolicy> publishPolicy(VideoSessionPolicy policy) async {
    final response = await _guarded(
      () => _apiClient.put('/api/v1/video-session/policy', body: policy.toJson()),
    );
    return VideoSessionPolicy.fromJson(_dataOf(response));
  }

  @override
  Future<List<VideoSessionDevice>> fetchDevices() async {
    final response = await _guarded(
      () => _apiClient.get('/api/v1/video-session/devices'),
    );
    final devices = _dataOf(response)['devices'];
    if (devices is! List) {
      return const <VideoSessionDevice>[];
    }
    return devices
        .whereType<Map>()
        .map(
          (item) => VideoSessionDevice.fromJson(Map<String, Object?>.from(item)),
        )
        .where((device) => device.deviceId.isNotEmpty)
        .toList(growable: false);
  }

  /// Runs one call and replaces a coded refusal with a guardian-facing one.
  ///
  /// The refusal code is the only durable part of a failure, so it is read and
  /// translated here. Nothing from the response body leaves this method.
  Future<T> _guarded<T>(Future<T> Function() call) async {
    try {
      return await call();
    } on AppException catch (error) {
      final refusal = _refusalFrom(error);
      if (refusal == null) {
        rethrow;
      }
      throw VideoSessionRefused(refusal);
    }
  }

  Map<String, Object?> _dataOf(Map<String, Object?> response) {
    final data = response['data'];
    return data is Map ? Map<String, Object?>.from(data) : <String, Object?>{};
  }
}

VideoSessionRefusal? _refusalFrom(AppException error) {
  return videoSessionRefusalOf(_causeBody(error));
}

/// Extracts the error body the transport attached to a mapped failure.
Object? _causeBody(AppException error) {
  final cause = error.cause;
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

/// Reads the refusal code from a raw response body, for tests and callers that
/// hold the transport error rather than the mapped one.
VideoSessionRefusal? videoSessionRefusalOf(Object? body) {
  return switch (readApiErrorCode(body)) {
    'invalid_policy' => VideoSessionRefusal.invalidPolicy,
    'service_error' => VideoSessionRefusal.unavailable,
    _ => null,
  };
}
