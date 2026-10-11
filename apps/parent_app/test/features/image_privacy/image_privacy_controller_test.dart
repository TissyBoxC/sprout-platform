import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/image_privacy/application/image_privacy_controller.dart';
import 'package:parent_app/features/image_privacy/data/image_privacy_api.dart';
import 'package:parent_app/features/image_privacy/domain/image_privacy.dart';

void main() {
  test('loads policy, catalogs and devices on first build', () async {
    final api = _FakeImagePrivacyApi(
      policy: _policy(enabled: false),
      devices: <ImagePrivacyDevice>[
        _device('device-1', uploadAllowed: true, upToDate: true),
        _device(
          'device-2',
          uploadAllowed: false,
          blocked: true,
          upToDate: false,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [imagePrivacyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(imagePrivacyControllerProvider.future);

    expect(state.policy.enabled, isFalse);
    expect(state.catalogs.processingModes, contains('local_full'));
    expect(state.devices, hasLength(2));
    expect(state.isEmpty, isFalse);
    expect(state.uploadCount, 1);
    expect(state.blockedCount, 1);
    expect(state.outdatedCount, 1);
  });

  test('publishes a policy then reloads', () async {
    final api = _FakeImagePrivacyApi(policy: _policy(enabled: false));
    final container = ProviderContainer(
      overrides: [imagePrivacyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(imagePrivacyControllerProvider.future);
    final controller = container.read(imagePrivacyControllerProvider.notifier);
    final published = await controller.publishPolicy(
      const ImagePrivacyDraft(
        enabled: true,
        uploadAllowed: false,
        localProcessingOnly: true,
        processingMode: 'metadata',
        uploadScope: 'none',
        retentionDays: 7,
        redactionLevel: 'strict',
        requireChildConsent: true,
      ),
    );

    expect(published.enabled, isTrue);
    expect(published.processingMode, 'metadata');
    final state = container.read(imagePrivacyControllerProvider).value;
    expect(state?.policy.enabled, isTrue);
    expect(api.publishCalls, 1);
    expect(api.fetchPolicyCalls, 2);
    expect(api.fetchDevicesCalls, 2);
  });

  test('reads a device capability through the controller', () async {
    final api = _FakeImagePrivacyApi(policy: _policy(enabled: true));
    final container = ProviderContainer(
      overrides: [imagePrivacyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(imagePrivacyControllerProvider.future);
    final capability = await container
        .read(imagePrivacyControllerProvider.notifier)
        .fetchCapability('device-1');

    expect(api.capabilityCalls, 1);
    expect(capability.supported, isTrue);
    expect(capability.upToDate, isFalse);
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeImagePrivacyApi(
      policy: _policy(enabled: false),
      failLoad: true,
    );
    final container = ProviderContainer(
      overrides: [imagePrivacyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      imagePrivacyControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(imagePrivacyControllerProvider).hasError, isTrue);
  });
}

ImagePrivacyPolicy _policy({required bool enabled}) {
  return ImagePrivacyPolicy(
    schemaVersion: 1,
    enabled: enabled,
    uploadAllowed: false,
    localProcessingOnly: true,
    processingMode: enabled ? 'metadata' : 'off',
    uploadScope: 'none',
    retentionDays: 0,
    redactionLevel: 'strict',
    requireChildConsent: true,
    revision: enabled ? 1 : 0,
  );
}

ImagePrivacyDevice _device(
  String id, {
  required bool uploadAllowed,
  bool blocked = false,
  bool upToDate = true,
}) {
  return ImagePrivacyDevice(
    deviceId: id,
    cameraSupported: true,
    localProcessing: true,
    uploadAllowed: uploadAllowed,
    processingMode: 'metadata',
    uploadScope: uploadAllowed ? 'thumbnails' : 'none',
    redactionLevel: 'strict',
    processedCount: 10,
    uploadedCount: uploadAllowed ? 2 : 0,
    blockedCount: blocked ? 1 : 0,
    lastBlockReason: blocked ? 'scope' : 'none',
    upToDate: upToDate,
  );
}

class _FakeImagePrivacyApi implements ImagePrivacyApi {
  _FakeImagePrivacyApi({
    required ImagePrivacyPolicy policy,
    List<ImagePrivacyDevice> devices = const <ImagePrivacyDevice>[],
    this.failLoad = false,
  }) : _policy = policy,
       _devices = <ImagePrivacyDevice>[...devices];

  ImagePrivacyPolicy _policy;
  final List<ImagePrivacyDevice> _devices;
  final bool failLoad;
  int publishCalls = 0;
  int fetchPolicyCalls = 0;
  int fetchDevicesCalls = 0;
  int capabilityCalls = 0;

  @override
  Future<ImagePrivacyConfig> fetchPolicy() async {
    if (failLoad) {
      throw Exception('network down');
    }
    fetchPolicyCalls += 1;
    return ImagePrivacyConfig(
      policy: _policy,
      catalogs: const ImagePrivacyCatalogs.conservative(),
    );
  }

  @override
  Future<List<ImagePrivacyDevice>> fetchDevices() async {
    if (failLoad) {
      throw Exception('network down');
    }
    fetchDevicesCalls += 1;
    return List<ImagePrivacyDevice>.unmodifiable(_devices);
  }

  @override
  Future<ImagePrivacyPolicy> publishPolicy(ImagePrivacyDraft draft) async {
    publishCalls += 1;
    _policy = ImagePrivacyPolicy(
      schemaVersion: 1,
      enabled: draft.enabled,
      uploadAllowed: draft.uploadAllowed,
      localProcessingOnly: draft.localProcessingOnly,
      processingMode: draft.processingMode,
      uploadScope: draft.uploadScope,
      retentionDays: draft.retentionDays,
      redactionLevel: draft.redactionLevel,
      requireChildConsent: draft.requireChildConsent,
      revision: _policy.revision + 1,
    );
    return _policy;
  }

  @override
  Future<ImagePrivacyCapability> fetchCapability(String deviceId) async {
    capabilityCalls += 1;
    return const ImagePrivacyCapability(
      supported: true,
      localProcessing: true,
      uploadAllowed: false,
      processingMode: 'metadata',
      uploadScope: 'none',
      redactionLevel: 'strict',
      schemaVersion: 1,
      policyRevision: 2,
      appliedRevision: 1,
      upToDate: false,
    );
  }
}
