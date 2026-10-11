import '../../../core/network/api_client.dart';
import '../domain/playback_request.dart';

/// Guardian-facing on-demand playback contracts.
///
/// The platform scopes every request to the authenticated family, so the
/// client never sends an owner id.
abstract interface class PlaybackApi {
  /// Lists the family's playback requests, newest first.
  ///
  /// Passing [deviceId] or [status] narrows the result.
  Future<List<PlaybackRequest>> fetchRequests({
    String? deviceId,
    String? status,
  });

  /// Queues one package to play on a bound device and returns the request.
  Future<PlaybackRequest> createRequest({
    required String deviceId,
    required String packageId,
    String note,
  });

  /// Cancels a still-pending request and returns the updated request.
  Future<PlaybackRequest> cancelRequest(String requestId);
}

/// Production implementation backed by the authenticated API client.
class HttpPlaybackApi implements PlaybackApi {
  const HttpPlaybackApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<List<PlaybackRequest>> fetchRequests({
    String? deviceId,
    String? status,
  }) async {
    final response = await _apiClient.get(
      '/api/v1/playback/requests',
      queryParameters: <String, String>{
        if (deviceId != null && deviceId.isNotEmpty) 'device_id': deviceId,
        if (status != null && status.isNotEmpty) 'status': status,
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing playback request data');
    }
    final requests = data['requests'];
    if (requests is! List) {
      return const <PlaybackRequest>[];
    }
    return requests
        .whereType<Map>()
        .map((item) => PlaybackRequest.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<PlaybackRequest> createRequest({
    required String deviceId,
    required String packageId,
    String note = '',
  }) async {
    final response = await _apiClient.post(
      '/api/v1/playback/requests',
      body: <String, Object?>{
        'device_id': deviceId,
        'package_id': packageId,
        'note': note,
      },
    );
    return _requiredRequest(response['data']);
  }

  @override
  Future<PlaybackRequest> cancelRequest(String requestId) async {
    final response = await _apiClient.post(
      '/api/v1/playback/requests/${Uri.encodeComponent(requestId)}/cancel',
    );
    return _requiredRequest(response['data']);
  }
}

PlaybackRequest _requiredRequest(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing playback request');
  }
  final envelope = Map<String, Object?>.from(data);
  final request = envelope['request'] ?? envelope;
  if (request is! Map) {
    throw const FormatException('missing playback request');
  }
  return PlaybackRequest.fromJson(Map<String, Object?>.from(request));
}
