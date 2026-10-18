import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/image_privacy_api.dart';
import '../domain/image_privacy.dart';

/// Complete state rendered by the image-privacy page.
class ImagePrivacyState {
  const ImagePrivacyState({
    required this.policy,
    required this.catalogs,
    required this.devices,
  });

  final ImagePrivacyPolicy policy;
  final ImagePrivacyCatalogs catalogs;
  final List<ImagePrivacyDevice> devices;

  bool get isEmpty => devices.isEmpty;

  int get uploadCount => devices.where((device) => device.uploadAllowed).length;

  int get blockedCount => devices.where((device) => device.blocked).length;

  int get outdatedCount => devices
      .where((device) => device.cameraSupported && !device.upToDate)
      .length;
}

/// Owns the family image-privacy policy so the page can read, publish and
/// refresh it without holding a device session.
class ImagePrivacyController extends AsyncNotifier<ImagePrivacyState> {
  late ImagePrivacyApi _api;

  @override
  Future<ImagePrivacyState> build() async {
    _api = ref.read(imagePrivacyApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Publishes a new policy revision and reloads the page state.
  Future<ImagePrivacyPolicy> publishPolicy(ImagePrivacyDraft draft) async {
    final policy = await _api.publishPolicy(draft);
    await refresh();
    return policy;
  }

  /// Reads one device capability for the detail sheet.
  Future<ImagePrivacyCapability> fetchCapability(String deviceId) {
    return _api.fetchCapability(deviceId);
  }

  Future<ImagePrivacyState> _load() async {
    final config = await _api.fetchPolicy();
    final devices = await _api.fetchDevices();
    return ImagePrivacyState(
      policy: config.policy,
      catalogs: config.catalogs,
      devices: devices,
    );
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final imagePrivacyApiProvider = Provider<ImagePrivacyApi>((ref) {
  return HttpImagePrivacyApi(ref.read(apiClientProvider));
});

final imagePrivacyControllerProvider =
    AsyncNotifierProvider<ImagePrivacyController, ImagePrivacyState>(
      ImagePrivacyController.new,
    );
