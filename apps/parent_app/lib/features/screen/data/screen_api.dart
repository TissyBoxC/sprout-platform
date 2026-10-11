import '../../../core/network/api_client.dart';
import '../domain/screen.dart';

/// Guardian-facing screen surface contract.
abstract interface class ScreenApi {
  Future<(ScreenPolicy, ScreenCatalog)> fetchPolicy();

  Future<ScreenPolicy> updatePolicy(ScreenPolicy policy);

  Future<List<PointReadEntry>> fetchDictionary();

  Future<PointReadEntry> putDictionaryEntry({
    required String term,
    required String text,
  });

  Future<void> deleteDictionaryEntry(String entryId);

  Future<VideoPlaybackPlan> fetchPlayback(String deviceId);

  Future<VideoPlaybackPlan> resolvePlayback({
    required String deviceId,
    required int bitrateKbps,
    required int fps,
  });
}

/// Production implementation backed by the authenticated API client.
class HttpScreenApi implements ScreenApi {
  const HttpScreenApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<(ScreenPolicy, ScreenCatalog)> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/screen/policy');
    final data = _data(response);
    return (
      ScreenPolicy.fromJson(_map(data['policy'])),
      ScreenCatalog.fromJson(data),
    );
  }

  @override
  Future<ScreenPolicy> updatePolicy(ScreenPolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/screen/policy',
      body: policy.toJson(),
    );
    return ScreenPolicy.fromJson(_data(response));
  }

  @override
  Future<List<PointReadEntry>> fetchDictionary() async {
    final response = await _apiClient.get('/api/v1/screen/point-read');
    final entries = _data(response)['entries'];
    if (entries is! List) {
      return const <PointReadEntry>[];
    }
    return entries
        .whereType<Map>()
        .map((item) => PointReadEntry.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<PointReadEntry> putDictionaryEntry({
    required String term,
    required String text,
  }) async {
    final response = await _apiClient.put(
      '/api/v1/screen/point-read',
      body: <String, Object?>{'term': term, 'text': text},
    );
    return PointReadEntry.fromJson(_data(response));
  }

  @override
  Future<void> deleteDictionaryEntry(String entryId) async {
    await _apiClient.delete('/api/v1/screen/point-read/$entryId');
  }

  @override
  Future<VideoPlaybackPlan> fetchPlayback(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/screen/playback',
    );
    return VideoPlaybackPlan.fromJson(_data(response));
  }

  @override
  Future<VideoPlaybackPlan> resolvePlayback({
    required String deviceId,
    required int bitrateKbps,
    required int fps,
  }) async {
    final response = await _apiClient.put(
      '/api/v1/devices/$deviceId/screen/playback',
      body: <String, Object?>{'bitrate_kbps': bitrateKbps, 'fps': fps},
    );
    return VideoPlaybackPlan.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing screen data');
  }
  return Map<String, Object?>.from(data);
}

Map<String, Object?> _map(Object? value) {
  if (value is! Map) {
    return const <String, Object?>{};
  }
  return Map<String, Object?>.from(value);
}
