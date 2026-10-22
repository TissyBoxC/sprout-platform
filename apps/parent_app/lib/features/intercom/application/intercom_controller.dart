import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../family/application/family_controller.dart';
import '../data/intercom_api.dart';

/// Resolves the signed-in family before a call can be addressed.
final intercomFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final intercomApiProvider = Provider<IntercomApi?>((ref) {
  final familyId = ref.watch(intercomFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    return null;
  }
  return HttpIntercomApi(ref.watch(apiClientProvider), familyId);
});

/// Drives one call from the guardian's side.
///
/// While a call is ringing or active the controller polls the platform until
/// the call reaches a terminal state, so the screen reflects the child's
/// accept or decline without the guardian pulling to refresh.
class IntercomCallController extends AsyncNotifier<DeviceCall?> {
  IntercomCallController(this.deviceId);

  final String deviceId;
  Timer? _poll;

  @override
  Future<DeviceCall?> build() async {
    ref.onDispose(() => _poll?.cancel());
    return null;
  }

  /// Rings the device and starts following the call.
  Future<DeviceCall> start() async {
    final api = await _api();
    final call = await api.start(deviceId);
    state = AsyncData(call);
    _schedulePoll();
    return call;
  }

  /// Stops a call the child has not answered yet.
  Future<void> cancel() async {
    final current = state.value;
    if (current == null) {
      return;
    }
    final api = await _api();
    final updated = await api.cancel(current.callId);
    state = AsyncData(updated);
    _stopPollingIfFinished(updated);
  }

  /// Ends an active call.
  Future<void> hangUp() async {
    final current = state.value;
    if (current == null) {
      return;
    }
    final api = await _api();
    final updated = await api.hangUp(current.callId);
    state = AsyncData(updated);
    _stopPollingIfFinished(updated);
  }

  /// Clears a finished call so the screen returns to its idle state.
  void reset() {
    _poll?.cancel();
    state = const AsyncData<DeviceCall?>(null);
  }

  void _schedulePoll() {
    _poll?.cancel();
    // Two seconds keeps the accept noticeable without hammering the platform;
    // the server independently enforces the ring timeout.
    _poll = Timer.periodic(const Duration(seconds: 2), (timer) async {
      final current = state.value;
      if (current == null || current.isFinished) {
        timer.cancel();
        return;
      }
      final api = ref.read(intercomApiProvider);
      if (api == null) {
        return;
      }
      // A failed poll must not clear the screen: the call may well still be
      // live, and showing "no call" mid-conversation would be misleading.
      try {
        final updated = await api.fetch(current.callId);
        state = AsyncData(updated);
        _stopPollingIfFinished(updated);
      } catch (_) {
        return;
      }
    });
  }

  void _stopPollingIfFinished(DeviceCall call) {
    if (call.isFinished) {
      _poll?.cancel();
    }
  }

  Future<IntercomApi> _api() async {
    await ref.read(intercomFamilyIdProvider.future);
    final api = ref.read(intercomApiProvider);
    if (api == null) {
      throw StateError('intercom api is not ready');
    }
    return api;
  }
}

final intercomCallControllerProvider =
    AsyncNotifierProvider.family<IntercomCallController, DeviceCall?, String>(
      IntercomCallController.new,
    );

/// Recent calls for the family, newest first.
final intercomHistoryProvider = FutureProvider<List<DeviceCall>>((ref) async {
  await ref.watch(intercomFamilyIdProvider.future);
  final api = ref.watch(intercomApiProvider);
  if (api == null) {
    return const <DeviceCall>[];
  }
  return api.history();
});
