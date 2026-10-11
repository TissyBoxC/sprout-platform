import '../../../core/network/api_client.dart';
import '../domain/wearable.dart';

/// Guardian-facing wearable contract.
abstract interface class WearableApi {
  Future<LocationPolicy> fetchPolicy();

  Future<LocationPolicy> updatePolicy(LocationPolicy policy);

  Future<List<EmergencyContact>> fetchContacts();

  Future<EmergencyContact> createContact(EmergencyContact contact);

  Future<EmergencyContact> updateContact(EmergencyContact contact);

  Future<void> deleteContact(String contactId);

  Future<HomeGeofence?> fetchGeofence();

  Future<HomeGeofence> updateGeofence(HomeGeofence geofence);

  Future<List<WearableDeviceStatus>> fetchDeviceStatuses();

  Future<List<SosEvent>> fetchActiveSos();

  Future<List<SosEvent>> fetchSosHistory();

  Future<SosEvent> cancelSos(String eventId);
}

/// Production implementation backed by the authenticated API client.
class HttpWearableApi implements WearableApi {
  const HttpWearableApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<LocationPolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/wearable/policy');
    return LocationPolicy.fromJson(_data(response));
  }

  @override
  Future<LocationPolicy> updatePolicy(LocationPolicy policy) async {
    final response = await _apiClient.put(
      '/api/v1/wearable/policy',
      body: policy.toJson(),
    );
    return LocationPolicy.fromJson(_data(response));
  }

  @override
  Future<List<EmergencyContact>> fetchContacts() async {
    final response = await _apiClient.get('/api/v1/wearable/contacts');
    final contacts = _data(response)['contacts'];
    if (contacts is! List) {
      return const <EmergencyContact>[];
    }
    return contacts
        .whereType<Map>()
        .map((item) => EmergencyContact.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<EmergencyContact> createContact(EmergencyContact contact) async {
    final response = await _apiClient.post(
      '/api/v1/wearable/contacts',
      body: contact.toJson(),
    );
    return EmergencyContact.fromJson(_data(response));
  }

  @override
  Future<EmergencyContact> updateContact(EmergencyContact contact) async {
    final response = await _apiClient.put(
      '/api/v1/wearable/contacts/${contact.id}',
      body: contact.toJson(),
    );
    return EmergencyContact.fromJson(_data(response));
  }

  @override
  Future<void> deleteContact(String contactId) async {
    await _apiClient.delete('/api/v1/wearable/contacts/$contactId');
  }

  @override
  Future<HomeGeofence?> fetchGeofence() async {
    final response = await _apiClient.get('/api/v1/wearable/geofence');
    final geofence = _data(response)['geofence'];
    if (geofence is! Map) {
      return null;
    }
    return HomeGeofence.fromJson(Map<String, Object?>.from(geofence));
  }

  @override
  Future<HomeGeofence> updateGeofence(HomeGeofence geofence) async {
    final response = await _apiClient.put(
      '/api/v1/wearable/geofence',
      body: geofence.toJson(),
    );
    return HomeGeofence.fromJson(_data(response));
  }

  @override
  Future<List<WearableDeviceStatus>> fetchDeviceStatuses() async {
    final response = await _apiClient.get('/api/v1/wearable/devices');
    final devices = _data(response)['devices'];
    if (devices is! List) {
      return const <WearableDeviceStatus>[];
    }
    return devices
        .whereType<Map>()
        .map(
          (item) => WearableDeviceStatus.fromJson(
            Map<String, Object?>.from(item),
          ),
        )
        .toList(growable: false);
  }

  @override
  Future<List<SosEvent>> fetchActiveSos() async {
    final response = await _apiClient.get('/api/v1/wearable/sos/active');
    return _sosEvents(_data(response));
  }

  @override
  Future<List<SosEvent>> fetchSosHistory() async {
    final response = await _apiClient.get('/api/v1/wearable/sos');
    return _sosEvents(_data(response));
  }

  @override
  Future<SosEvent> cancelSos(String eventId) async {
    final response = await _apiClient.put(
      '/api/v1/wearable/sos/$eventId/cancel',
    );
    return SosEvent.fromJson(_data(response));
  }
}

List<SosEvent> _sosEvents(Map<String, Object?> data) {
  final events = data['events'];
  if (events is! List) {
    return const <SosEvent>[];
  }
  return events
      .whereType<Map>()
      .map((item) => SosEvent.fromJson(Map<String, Object?>.from(item)))
      .toList(growable: false);
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing wearable data');
  }
  return Map<String, Object?>.from(data);
}
