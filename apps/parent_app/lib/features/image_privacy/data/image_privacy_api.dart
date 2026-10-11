import '../../../core/network/api_client.dart';
import '../domain/image_privacy.dart';

/// Guardian-facing image-privacy contracts. Every call is family-scoped on the
/// platform, so the client never sends an owner id.
abstract interface class ImagePrivacyApi {
  /// Reads the family policy and its fixed editor catalogs.
  Future<ImagePrivacyConfig> fetchPolicy();

  /// Publishes a new family policy revision and returns the stored policy.
  Future<ImagePrivacyPolicy> publishPolicy(ImagePrivacyDraft draft);

  /// Lists each family device's latest bounded image-processing status.
  Future<List<ImagePrivacyDevice>> fetchDevices();

  /// Reads one family device's current image-privacy capability.
  Future<ImagePrivacyCapability> fetchCapability(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpImagePrivacyApi implements ImagePrivacyApi {
  const HttpImagePrivacyApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<ImagePrivacyConfig> fetchPolicy() async {
    final response = await _apiClient.get('/api/v1/image-privacy/policy');
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing image privacy policy');
    }
    return ImagePrivacyConfig(
      policy: _requiredPolicy(data),
      catalogs: ImagePrivacyCatalogs.fromJson(Map<String, Object?>.from(data)),
    );
  }

  @override
  Future<ImagePrivacyPolicy> publishPolicy(ImagePrivacyDraft draft) async {
    final response = await _apiClient.put(
      '/api/v1/image-privacy/policy',
      body: draft.toJson(),
    );
    return _requiredPolicy(response['data']);
  }

  @override
  Future<List<ImagePrivacyDevice>> fetchDevices() async {
    final response = await _apiClient.get('/api/v1/image-privacy/devices');
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing image privacy devices');
    }
    final devices = data['devices'];
    if (devices is! List) {
      return const <ImagePrivacyDevice>[];
    }
    return devices
        .whereType<Map>()
        .map(
          (item) =>
              ImagePrivacyDevice.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<ImagePrivacyCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/${Uri.encodeComponent(deviceId)}/image-privacy/capability',
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing image privacy capability');
    }
    return ImagePrivacyCapability.fromJson(Map<String, Object?>.from(data));
  }
}

ImagePrivacyPolicy _requiredPolicy(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing image privacy policy');
  }
  final envelope = Map<String, Object?>.from(data);
  final policy = envelope['policy'] ?? envelope;
  if (policy is! Map) {
    throw const FormatException('missing image privacy policy');
  }
  return ImagePrivacyPolicy.fromJson(Map<String, Object?>.from(policy));
}
