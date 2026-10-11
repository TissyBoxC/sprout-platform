import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/image_privacy/application/image_privacy_controller.dart';
import 'package:parent_app/features/image_privacy/domain/image_privacy.dart';
import 'package:parent_app/features/image_privacy/presentation/image_privacy_page.dart';

void main() {
  testWidgets('shows the policy summary and device status', (tester) async {
    await tester.pumpWidget(
      _app(
        _FakeImagePrivacyController(
          ImagePrivacyState(
            policy: _policy(
              enabled: true,
              uploadAllowed: true,
              localProcessingOnly: false,
            ),
            catalogs: const ImagePrivacyCatalogs.conservative(),
            devices: <ImagePrivacyDevice>[
              _device('device-1', uploadAllowed: true, upToDate: true),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('图片隐私'), findsOneWidget);
    expect(find.text('图片隐私设置'), findsOneWidget);
    expect(find.text('本地完整处理'), findsOneWidget);
    expect(find.text('仅缩略图'), findsOneWidget);
    expect(find.text('设备状态'), findsOneWidget);
    expect(find.text('可上传 1 · 已拦截 0'), findsOneWidget);
  });

  testWidgets('shows the empty device state', (tester) async {
    await tester.pumpWidget(
      _app(
        _FakeImagePrivacyController(
          ImagePrivacyState(
            policy: const ImagePrivacyPolicy.conservative(),
            catalogs: const ImagePrivacyCatalogs.conservative(),
            devices: const <ImagePrivacyDevice>[],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('未开启：设备不会处理或上传图片。'), findsOneWidget);
    expect(find.text('还没有设备上报图片处理状态。'), findsOneWidget);
  });

  testWidgets('does not imply upload is allowed when it is disabled', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        _FakeImagePrivacyController(
          ImagePrivacyState(
            policy: _policy(
              enabled: true,
              uploadAllowed: false,
              localProcessingOnly: false,
            ),
            catalogs: const ImagePrivacyCatalogs.conservative(),
            devices: const <ImagePrivacyDevice>[],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('不可上传'), findsOneWidget);
    expect(find.text('允许上传'), findsNothing);
  });

  testWidgets('keeps local-only editing mutually exclusive', (tester) async {
    final controller = _FakeImagePrivacyController(
      ImagePrivacyState(
        policy: _policy(
          enabled: true,
          uploadAllowed: true,
          localProcessingOnly: false,
        ),
        catalogs: const ImagePrivacyCatalogs.conservative(),
        devices: const <ImagePrivacyDevice>[],
      ),
    );
    await tester.pumpWidget(_app(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('编辑设置'));
    await tester.pumpAndSettle();

    expect(find.text('图片隐私设置'), findsWidgets);
    await tester.scrollUntilVisible(
      find.text('仅在本机处理'),
      300,
      scrollable: find.byType(Scrollable).last,
    );
    await tester.tap(find.text('仅在本机处理'));
    await tester.pumpAndSettle();

    final uploadSwitch = tester.widget<SwitchListTile>(
      find.widgetWithText(SwitchListTile, '允许上传'),
    );
    expect(uploadSwitch.value, isFalse);
    expect(uploadSwitch.onChanged, isNull);
  });

  testWidgets('saves an edited policy and reports success', (tester) async {
    final controller = _FakeImagePrivacyController(
      ImagePrivacyState(
        policy: const ImagePrivacyPolicy.conservative(),
        catalogs: const ImagePrivacyCatalogs.conservative(),
        devices: const <ImagePrivacyDevice>[],
      ),
    );
    await tester.pumpWidget(_app(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('编辑设置'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('保存设置'),
      300,
      scrollable: find.byType(Scrollable).last,
    );
    await tester.tap(find.text('保存设置'));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.retentionDays, 0);
    expect(find.text('图片隐私设置已保存'), findsOneWidget);
  });

  testWidgets('enabling from the summary selects an active mode', (
    tester,
  ) async {
    final controller = _FakeImagePrivacyController(
      ImagePrivacyState(
        policy: const ImagePrivacyPolicy.conservative(),
        catalogs: const ImagePrivacyCatalogs.conservative(),
        devices: const <ImagePrivacyDevice>[],
      ),
    );
    await tester.pumpWidget(_app(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.enabled, isTrue);
    expect(controller.saved!.processingMode, 'metadata');
    expect(controller.saved!.uploadAllowed, isFalse);
    expect(controller.saved!.uploadScope, 'none');
  });
}

Widget _app(_FakeImagePrivacyController controller) {
  return ProviderScope(
    overrides: [imagePrivacyControllerProvider.overrideWith(() => controller)],
    child: const MaterialApp(home: ImagePrivacyPage()),
  );
}

ImagePrivacyPolicy _policy({
  required bool enabled,
  bool uploadAllowed = false,
  bool localProcessingOnly = true,
}) {
  return ImagePrivacyPolicy(
    schemaVersion: 1,
    enabled: enabled,
    uploadAllowed: uploadAllowed,
    localProcessingOnly: localProcessingOnly,
    processingMode: enabled
        ? (localProcessingOnly ? 'metadata' : 'local_full')
        : 'off',
    uploadScope: uploadAllowed ? 'thumbnails' : 'none',
    retentionDays: 0,
    redactionLevel: 'strict',
    requireChildConsent: true,
    revision: 1,
  );
}

ImagePrivacyDevice _device(
  String id, {
  required bool uploadAllowed,
  required bool upToDate,
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
    blockedCount: 0,
    lastBlockReason: 'none',
    upToDate: upToDate,
  );
}

class _FakeImagePrivacyController extends ImagePrivacyController {
  _FakeImagePrivacyController(this.value);

  ImagePrivacyState value;
  ImagePrivacyDraft? saved;

  @override
  Future<ImagePrivacyState> build() async => value;

  @override
  Future<void> refresh() async {}

  @override
  Future<ImagePrivacyPolicy> publishPolicy(ImagePrivacyDraft draft) async {
    saved = draft;
    value = ImagePrivacyState(
      policy: ImagePrivacyPolicy(
        schemaVersion: 1,
        enabled: draft.enabled,
        uploadAllowed: draft.uploadAllowed,
        localProcessingOnly: draft.localProcessingOnly,
        processingMode: draft.processingMode,
        uploadScope: draft.uploadScope,
        retentionDays: draft.retentionDays,
        redactionLevel: draft.redactionLevel,
        requireChildConsent: draft.requireChildConsent,
        revision: value.policy.revision + 1,
      ),
      catalogs: value.catalogs,
      devices: value.devices,
    );
    return value.policy;
  }

  @override
  Future<ImagePrivacyCapability> fetchCapability(String deviceId) async {
    return const ImagePrivacyCapability(
      supported: true,
      localProcessing: true,
      uploadAllowed: false,
      processingMode: 'metadata',
      uploadScope: 'none',
      redactionLevel: 'strict',
      schemaVersion: 1,
      policyRevision: 1,
      appliedRevision: 1,
      upToDate: true,
    );
  }
}
