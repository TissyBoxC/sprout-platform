import '../../../core/network/api_client.dart';
import '../domain/bluetooth_audio.dart';

/// Guardian-facing Bluetooth audio contracts. Every call is family-scoped on
/// the platform, so the client never sends an owner id.
abstract interface class BluetoothAudioApi {
  /// Reads the family policy, or the platform default when none is set.
  Future<BluetoothAudioPolicy> fetchPolicy();

  /// Publishes a new family policy revision and returns the stored policy.
  Future<BluetoothAudioPolicy> publishPolicy(BluetoothAudioDraft draft);

  /// Lists each family device's latest bounded adapter status.
  Future<List<BluetoothAudioDevice>> fetchDevices();
}

/// Production implementation backed by the authenticated API client.
class HttpBluetoothAudioApi implements BluetoothAudioApi {
  const HttpBluetoothAudioApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<BluetoothAudioPolicy> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/bluetooth-audio/policy');
    return _requiredPolicy(response['data']);
  }

  @override
  Future<BluetoothAudioPolicy> publishPolicy(BluetoothAudioDraft draft) async {
    final response = await _apiClient.put(
      '/api/v1/bluetooth-audio/policy',
      body: draft.toJson(),
    );
    return _requiredPolicy(response['data']);
  }

  @override
  Future<List<BluetoothAudioDevice>> fetchDevices() async {
    final response = await _apiClient.get('/api/v1/bluetooth-audio/devices');
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing bluetooth audio data');
    }
    final devices = data['devices'];
    if (devices is! List) {
      return const <BluetoothAudioDevice>[];
    }
    return devices
        .whereType<Map>()
        .map(
          (item) =>
              BluetoothAudioDevice.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }
}

BluetoothAudioPolicy _requiredPolicy(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing bluetooth audio policy');
  }
  final envelope = Map<String, Object?>.from(data);
  final policy = envelope['policy'] ?? envelope;
  if (policy is! Map) {
    throw const FormatException('missing bluetooth audio policy');
  }
  return BluetoothAudioPolicy.fromJson(Map<String, Object?>.from(policy));
}
