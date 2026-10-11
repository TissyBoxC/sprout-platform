import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/bluetooth_audio_api.dart';
import '../domain/bluetooth_audio.dart';

/// Complete state rendered by the Bluetooth audio page.
class BluetoothAudioState {
  const BluetoothAudioState({required this.policy, required this.devices});

  final BluetoothAudioPolicy policy;
  final List<BluetoothAudioDevice> devices;

  bool get isEmpty => devices.isEmpty;

  int get connectedCount => devices.where((device) => device.connected).length;

  int get outdatedCount =>
      devices.where((device) => device.supported && !device.upToDate).length;
}

/// Owns the family Bluetooth audio policy so the page can read, publish and
/// refresh it without holding a device session.
class BluetoothAudioController extends AsyncNotifier<BluetoothAudioState> {
  late final BluetoothAudioApi _api;

  @override
  Future<BluetoothAudioState> build() async {
    _api = ref.read(bluetoothAudioApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Publishes a new policy revision and reloads the page state.
  Future<BluetoothAudioPolicy> publishPolicy(BluetoothAudioDraft draft) async {
    final policy = await _api.publishPolicy(draft);
    await refresh();
    return policy;
  }

  Future<BluetoothAudioState> _load() async {
    final policy = await _api.fetchPolicy();
    final devices = await _api.fetchDevices();
    return BluetoothAudioState(policy: policy, devices: devices);
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final bluetoothAudioApiProvider = Provider<BluetoothAudioApi>((ref) {
  return HttpBluetoothAudioApi(ref.read(apiClientProvider));
});

final bluetoothAudioControllerProvider =
    AsyncNotifierProvider<BluetoothAudioController, BluetoothAudioState>(
      BluetoothAudioController.new,
    );
