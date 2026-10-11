import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/bluetooth_audio/application/bluetooth_audio_controller.dart';
import 'package:parent_app/features/bluetooth_audio/data/bluetooth_audio_api.dart';
import 'package:parent_app/features/bluetooth_audio/domain/bluetooth_audio.dart';

void main() {
  test('loads the policy and devices on first build', () async {
    final api = _FakeBluetoothAudioApi(
      policy: _policy(enabled: false),
      devices: [
        _device('device-1', 'connected', upToDate: true),
        _device('device-2', 'off', upToDate: false),
      ],
    );
    final container = ProviderContainer(
      overrides: [bluetoothAudioApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(bluetoothAudioControllerProvider.future);

    expect(state.policy.enabled, isFalse);
    expect(state.devices, hasLength(2));
    expect(state.isEmpty, isFalse);
    expect(state.connectedCount, 1);
    expect(state.outdatedCount, 1);
  });

  test('publishes a policy then reloads', () async {
    final api = _FakeBluetoothAudioApi(policy: _policy(enabled: false));
    final container = ProviderContainer(
      overrides: [bluetoothAudioApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(bluetoothAudioControllerProvider.future);
    final controller = container.read(
      bluetoothAudioControllerProvider.notifier,
    );
    final published = await controller.publishPolicy(
      const BluetoothAudioDraft(
        enabled: true,
        speakerModeAllowed: true,
        pairingAllowed: true,
        autoReconnect: true,
        preferredCodec: BluetoothCodec.ldac,
        maxVolumePercent: 80,
        displayName: '客厅音箱',
      ),
    );

    expect(published.enabled, isTrue);
    expect(published.preferredCodec, BluetoothCodec.ldac);
    final state = container.read(bluetoothAudioControllerProvider).value;
    expect(state?.policy.enabled, isTrue);
    expect(api.publishCalls, 1);
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeBluetoothAudioApi(
      policy: _policy(enabled: false),
      failLoad: true,
    );
    final container = ProviderContainer(
      overrides: [bluetoothAudioApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      bluetoothAudioControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(bluetoothAudioControllerProvider).hasError, isTrue);
  });
}

BluetoothAudioPolicy _policy({required bool enabled}) {
  return BluetoothAudioPolicy(
    enabled: enabled,
    speakerModeAllowed: enabled,
    pairingAllowed: enabled,
    autoReconnect: true,
    preferredCodec: BluetoothCodec.auto,
    maxVolumePercent: 70,
    displayName: '',
    revision: enabled ? 1 : 0,
  );
}

BluetoothAudioDevice _device(
  String id,
  String adapterState, {
  required bool upToDate,
}) {
  return BluetoothAudioDevice(
    deviceId: id,
    supported: true,
    speakerMode: adapterState == 'connected',
    adapterState: adapterState,
    pairedDeviceCount: adapterState == 'connected' ? 1 : 0,
    connectedDeviceCount: adapterState == 'connected' ? 1 : 0,
    activeCodec: adapterState == 'connected' ? 'aac' : 'unknown',
    volumePercent: adapterState == 'connected' ? 40 : 0,
    lastDisconnectReason: 'none',
    upToDate: upToDate,
  );
}

class _FakeBluetoothAudioApi implements BluetoothAudioApi {
  _FakeBluetoothAudioApi({
    required BluetoothAudioPolicy policy,
    List<BluetoothAudioDevice> devices = const [],
    this.failLoad = false,
  }) : _policy = policy,
       _devices = [...devices];

  BluetoothAudioPolicy _policy;
  final List<BluetoothAudioDevice> _devices;
  final bool failLoad;
  int publishCalls = 0;

  @override
  Future<BluetoothAudioPolicy> fetchPolicy() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return _policy;
  }

  @override
  Future<List<BluetoothAudioDevice>> fetchDevices() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return List<BluetoothAudioDevice>.unmodifiable(_devices);
  }

  @override
  Future<BluetoothAudioPolicy> publishPolicy(BluetoothAudioDraft draft) async {
    publishCalls += 1;
    _policy = BluetoothAudioPolicy(
      enabled: draft.enabled,
      speakerModeAllowed: draft.speakerModeAllowed,
      pairingAllowed: draft.pairingAllowed,
      autoReconnect: draft.autoReconnect,
      preferredCodec: draft.preferredCodec,
      maxVolumePercent: draft.maxVolumePercent,
      displayName: draft.displayName,
      revision: _policy.revision + 1,
    );
    return _policy;
  }

  @override
  Future<Map<String, Object?>> fetchAdminStats({int windowDays = 7}) async {
    return <String, Object?>{'window_days': windowDays};
  }
}
