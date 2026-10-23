import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/bluetooth_audio/data/bluetooth_audio_api.dart';
import 'package:parent_app/features/bluetooth_audio/domain/bluetooth_audio.dart';

void main() {
  test('parses the policy envelope with a conservative default', () async {
    final api = HttpBluetoothAudioApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'policy': <String, Object?>{
              'enabled': false,
              'speaker_mode_allowed': false,
              'pairing_allowed': false,
              'auto_reconnect': true,
              'preferred_codec': 'auto',
              'max_volume_percent': 70,
              'display_name': '',
              'revision': 0,
            },
          },
        },
      ),
    );

    final policy = await api.fetchPolicy();

    expect(policy.enabled, isFalse);
    expect(policy.pairingAllowed, isFalse);
    expect(policy.preferredCodec, BluetoothCodec.auto);
    expect(policy.maxVolumePercent, 70);
    expect(policy.revision, 0);
  });

  test('publishes a policy with the draft JSON body', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'policy': <String, Object?>{
            'enabled': true,
            'speaker_mode_allowed': true,
            'pairing_allowed': true,
            'auto_reconnect': false,
            'preferred_codec': 'aac',
            'max_volume_percent': 60,
            'display_name': '客厅音箱',
            'revision': 2,
          },
        },
      },
    );
    final api = HttpBluetoothAudioApi(client);

    final policy = await api.publishPolicy(
      const BluetoothAudioDraft(
        enabled: true,
        speakerModeAllowed: true,
        pairingAllowed: true,
        autoReconnect: false,
        preferredCodec: BluetoothCodec.aac,
        maxVolumePercent: 60,
        displayName: '客厅音箱',
      ),
    );

    expect(policy.preferredCodec, BluetoothCodec.aac);
    expect(policy.revision, 2);
    expect(client.lastPutPath, '/api/v1/bluetooth-audio/policy');
    expect(client.lastPutBody, <String, Object?>{
      'enabled': true,
      'speaker_mode_allowed': true,
      'pairing_allowed': true,
      'auto_reconnect': false,
      'preferred_codec': 'aac',
      'max_volume_percent': 60,
      'display_name': '客厅音箱',
    });
  });

  test('parses device status rows', () async {
    final api = HttpBluetoothAudioApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'devices': <Object?>[
              <String, Object?>{
                'device_id': 'device-1',
                'bluetooth_supported': true,
                'speaker_mode': true,
                'adapter_state': 'connected',
                'paired_device_count': 2,
                'connected_device_count': 1,
                'active_codec': 'aac',
                'volume_percent': 40,
                'last_disconnect_reason': 'none',
                'up_to_date': true,
              },
            ],
          },
        },
      ),
    );

    final devices = await api.fetchDevices();

    expect(devices, hasLength(1));
    expect(devices.single.connected, isTrue);
    expect(devices.single.activeCodec, 'aac');
    expect(devices.single.pairedDeviceCount, 2);
    expect(devices.single.upToDate, isTrue);
    expect(adapterStateLabel('connected'), '已连接');
    expect(activeCodecLabel('aac'), 'AAC');
  });

  test('defaults unknown codec and adapter labels safely', () {
    expect(BluetoothCodec.fromApiValue('aptx'), BluetoothCodec.auto);
    expect(adapterStateLabel('sleeping'), '未知');
    expect(disconnectReasonLabel('exploded'), '未知');
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  String? lastPutPath;
  Object? lastPutBody;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async =>
      response;

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    lastPutPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async => response;

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
