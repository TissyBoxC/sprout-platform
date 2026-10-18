import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/wearable_api.dart';
import '../domain/wearable.dart';

/// Holds the family location policy, emergency contacts, home geofence, device
/// snapshots and the SOS history.
class WearableState {
  const WearableState({
    required this.policy,
    required this.contacts,
    required this.geofence,
    required this.devices,
    required this.activeSos,
    required this.sosHistory,
  });

  final LocationPolicy policy;
  final List<EmergencyContact> contacts;
  final HomeGeofence? geofence;
  final List<WearableDeviceStatus> devices;
  final List<SosEvent> activeSos;
  final List<SosEvent> sosHistory;

  bool get hasActiveSos => activeSos.isNotEmpty;

  WearableState copyWith({
    LocationPolicy? policy,
    List<EmergencyContact>? contacts,
    HomeGeofence? geofence,
    bool clearGeofence = false,
    List<WearableDeviceStatus>? devices,
    List<SosEvent>? activeSos,
    List<SosEvent>? sosHistory,
  }) {
    return WearableState(
      policy: policy ?? this.policy,
      contacts: contacts ?? this.contacts,
      geofence: clearGeofence ? null : (geofence ?? this.geofence),
      devices: devices ?? this.devices,
      activeSos: activeSos ?? this.activeSos,
      sosHistory: sosHistory ?? this.sosHistory,
    );
  }
}

/// Owns the wearable location settings for the family.
class WearableController extends AsyncNotifier<WearableState> {
  late WearableApi _api;

  @override
  Future<WearableState> build() async {
    _api = ref.read(wearableApiProvider);
    final policy = await _api.fetchPolicy();
    final contacts = await _api.fetchContacts();
    final geofence = await _api.fetchGeofence();
    final devices = await _api.fetchDeviceStatuses();
    final active = await _api.fetchActiveSos();
    final history = await _api.fetchSosHistory();
    return WearableState(
      policy: policy,
      contacts: contacts,
      geofence: geofence,
      devices: devices,
      activeSos: active,
      sosHistory: history,
    );
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(build);
  }

  Future<void> updatePolicy(LocationPolicy policy) async {
    final saved = await _api.updatePolicy(policy);
    final current = state.value;
    if (current != null) {
      // A revoked permission clears the retained device positions server-side,
      // so refresh the device list to reflect the cleared state.
      final devices = saved.permissionGranted
          ? current.devices
          : const <WearableDeviceStatus>[];
      state = AsyncValue.data(
        current.copyWith(policy: saved, devices: devices),
      );
    }
  }

  Future<EmergencyContact> createContact(EmergencyContact contact) async {
    final created = await _api.createContact(contact);
    final current = state.value;
    if (current != null) {
      state = AsyncValue.data(
        current.copyWith(
          contacts: <EmergencyContact>[created, ...current.contacts],
        ),
      );
    }
    return created;
  }

  Future<void> deleteContact(String contactId) async {
    await _api.deleteContact(contactId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        contacts: current.contacts
            .where((contact) => contact.id != contactId)
            .toList(growable: false),
      ),
    );
  }

  Future<void> updateGeofence(HomeGeofence geofence) async {
    final saved = await _api.updateGeofence(geofence);
    final current = state.value;
    if (current != null) {
      state = AsyncValue.data(current.copyWith(geofence: saved));
    }
  }

  Future<void> cancelSos(String eventId) async {
    await _api.cancelSos(eventId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        activeSos: current.activeSos
            .where((event) => event.id != eventId)
            .toList(growable: false),
      ),
    );
  }

  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final wearableApiProvider = Provider<WearableApi>((ref) {
  return HttpWearableApi(ref.read(apiClientProvider));
});

final wearableControllerProvider =
    AsyncNotifierProvider<WearableController, WearableState>(
      WearableController.new,
    );
