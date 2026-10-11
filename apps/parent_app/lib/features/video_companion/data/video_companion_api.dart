import '../../../core/network/api_client.dart';
import '../domain/video_companion.dart';

/// Guardian-facing video companionship contract.
abstract interface class VideoCompanionApi {
  Future<List<VideoContact>> fetchContacts();

  Future<VideoContact> createContact(VideoContact contact);

  Future<VideoContact> updateContact(VideoContact contact);

  Future<void> deleteContact(String contactId);

  Future<List<VideoCall>> fetchHistory();

  Future<VideoCall> startCall({
    required String deviceId,
    required String contactId,
  });

  Future<VideoCall> endCall(String callId);

  Future<VideoCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpVideoCompanionApi implements VideoCompanionApi {
  const HttpVideoCompanionApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<List<VideoContact>> fetchContacts() async {
    final response = await _apiClient.get('/api/v1/video-companion/contacts');
    final data = _data(response);
    final contacts = data['contacts'];
    if (contacts is! List) {
      return const <VideoContact>[];
    }
    return contacts
        .whereType<Map>()
        .map((item) => VideoContact.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<VideoContact> createContact(VideoContact contact) async {
    final response = await _apiClient.post(
      '/api/v1/video-companion/contacts',
      body: contact.toJson(),
    );
    return VideoContact.fromJson(_data(response));
  }

  @override
  Future<VideoContact> updateContact(VideoContact contact) async {
    final response = await _apiClient.put(
      '/api/v1/video-companion/contacts/${contact.id}',
      body: contact.toJson(),
    );
    return VideoContact.fromJson(_data(response));
  }

  @override
  Future<void> deleteContact(String contactId) async {
    await _apiClient.delete('/api/v1/video-companion/contacts/$contactId');
  }

  @override
  Future<List<VideoCall>> fetchHistory() async {
    final response = await _apiClient.get('/api/v1/video-companion/calls');
    final data = _data(response);
    final calls = data['calls'];
    if (calls is! List) {
      return const <VideoCall>[];
    }
    return calls
        .whereType<Map>()
        .map((item) => VideoCall.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<VideoCall> startCall({
    required String deviceId,
    required String contactId,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/video-companion/calls',
      body: <String, Object?>{
        'device_id': deviceId,
        'contact_id': contactId,
        'initiator': 'guardian',
      },
    );
    return VideoCall.fromJson(_data(response));
  }

  @override
  Future<VideoCall> endCall(String callId) async {
    final response = await _apiClient.put(
      '/api/v1/video-companion/calls/$callId',
      body: <String, Object?>{
        'state': 'ended',
        'quality': 'unknown',
        'end_reason': 'hangup',
      },
    );
    return VideoCall.fromJson(_data(response));
  }

  @override
  Future<VideoCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/video-companion/capability',
    );
    return VideoCapability.fromJson(_data(response));
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing video companion data');
  }
  return Map<String, Object?>.from(data);
}
