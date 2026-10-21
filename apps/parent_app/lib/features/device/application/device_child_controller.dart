import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../family/application/family_controller.dart';
import '../data/device_child_api.dart';

/// Resolves the signed-in family before a device's child can be addressed.
final deviceChildFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final deviceChildApiProvider = Provider<DeviceChildApi?>((ref) {
  final familyId = ref.watch(deviceChildFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    return null;
  }
  return HttpDeviceChildApi(ref.watch(apiClientProvider), familyId);
});

/// Loads and mutates one device's current child.
///
/// Each device gets its own controller so two devices open in the same session
/// cannot overwrite each other's selected child.
class DeviceChildController extends AsyncNotifier<DeviceChildStatus> {
  DeviceChildController(this.deviceId);

  final String deviceId;
  int _requestGeneration = 0;

  @override
  Future<DeviceChildStatus> build() async {
    return _load();
  }

  Future<DeviceChildStatus> _load() async {
    final api = await _api();
    return api.fetchStatus(deviceId);
  }

  /// Reloads the device's current child.
  Future<void> refresh() async {
    final generation = ++_requestGeneration;
    state = const AsyncValue<DeviceChildStatus>.loading();
    final next = await AsyncValue.guard(_load);
    if (generation == _requestGeneration) {
      state = next;
    }
  }

  /// Makes a child the device's current child.
  ///
  /// The status is reloaded instead of patched locally so the screen shows what
  /// the platform actually stored, including a switch that raced another
  /// guardian's change on a second phone.
  Future<void> activate(String childId) async {
    final api = await _api();
    await api.activate(deviceId: deviceId, childId: childId);
    await refresh();
  }

  /// Clears the device's current child.
  Future<void> clear() async {
    final api = await _api();
    await api.clear(deviceId);
    await refresh();
  }

  Future<DeviceChildApi> _api() async {
    await ref.read(deviceChildFamilyIdProvider.future);
    final api = ref.read(deviceChildApiProvider);
    if (api == null) {
      throw StateError('device child api is not ready');
    }
    return api;
  }
}

final deviceChildControllerProvider =
    AsyncNotifierProvider.family<DeviceChildController, DeviceChildStatus, String>(
      DeviceChildController.new,
    );

/// The children a device has served, used only on the history sheet.
final deviceChildHistoryProvider =
    FutureProvider.family<List<DeviceChildHistoryEntry>, String>((
      ref,
      deviceId,
    ) async {
      await ref.watch(deviceChildFamilyIdProvider.future);
      final api = ref.watch(deviceChildApiProvider);
      if (api == null) {
        return const <DeviceChildHistoryEntry>[];
      }
      return api.fetchHistory(deviceId);
    });
