import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/error/api_error_mapper.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';
import 'package:parent_app/features/video_companion/application/video_companion_controller.dart';
import 'package:parent_app/features/video_companion/domain/video_companion.dart';
import 'package:parent_app/features/video_session/application/video_session_controller.dart';
import 'package:parent_app/features/video_session/data/video_session_api.dart';
import 'package:parent_app/features/video_session/domain/video_session.dart';
import 'package:parent_app/features/video_session/presentation/video_session_copy.dart';
import 'package:parent_app/features/video_session/presentation/video_session_page.dart';

void main() {
  group('读取设置', () {
    testWidgets('shows the stored permissions and the privacy boundary', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi()));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.pageTitle), findsOneWidget);
      // Capture is opt-in: the stored policy allows the camera but not the
      // microphone, and the screen states both.
      expect(_switchValue(tester, 'video-session-camera'), isTrue);
      expect(_switchValue(tester, 'video-session-microphone'), isFalse);
      expect(find.text(VideoSessionCopy.privacyBody), findsOneWidget);
      expect(find.text(VideoSessionCopy.noRecordingBody), findsOneWidget);
      expect(find.text(VideoSessionCopy.captureBlockedHint), findsNothing);
    });

    testWidgets('marks an in-effect setting as unchanged and not saveable', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi()));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.noChangeHint), findsOneWidget);
      final button = tester.widget<FilledButton>(
        find.byKey(const ValueKey<String>('video-session-save')),
      );
      expect(button.onPressed, isNull);
    });

    testWidgets('shows a loading sentence before the policy arrives', (
      tester,
    ) async {
      final api = _FakeVideoSessionApi(pending: true);
      _expand(tester);
      await tester.pumpWidget(_page(api: api));
      await tester.pump();

      expect(
        find.byKey(const ValueKey<String>('video-session-loading')),
        findsOneWidget,
      );
      expect(find.text(VideoSessionCopy.loading), findsWidgets);
      expect(
        find.byKey(const ValueKey<String>('video-session-save')),
        findsNothing,
      );

      api.release();
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey<String>('video-session-save')),
        findsOneWidget,
      );
    });

    testWidgets('names the device when it cannot hold a call', (tester) async {
      _unsupportedDevices.add('device-1');
      addTearDown(_unsupportedDevices.clear);
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi()));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.capabilityUnsupported), findsOneWidget);
    });

    testWidgets('stays quiet when the device reports support', (tester) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi()));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.capabilityUnsupported), findsNothing);
    });
  });

  group('保存设置', () {
    testWidgets('allows the microphone and reports the stored result', (
      tester,
    ) async {
      _expand(tester);
      final api = _FakeVideoSessionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-microphone')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('video-session-save')));
      await tester.pumpAndSettle();

      expect(api.saved.map((policy) => policy.microphoneAllowed), <bool>[true]);
      expect(find.text(VideoSessionCopy.saveSucceeded), findsOneWidget);
      expect(_switchValue(tester, 'video-session-microphone'), isTrue);
      // The revision the platform returned is the one the screen now holds.
      expect(api.lastSavedRevision, 4);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('keeps the draft and explains a refused save', (tester) async {
      _expand(tester);
      final api = _FakeVideoSessionApi(refuseSave: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-camera')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('video-session-save')));
      await tester.pumpAndSettle();

      expect(
        find.text(
          '${VideoSessionCopy.invalidPolicy}${VideoSessionCopy.invalidPolicyHint}',
        ),
        findsOneWidget,
      );
      // The guardian's draft is not thrown away by a refusal.
      expect(_switchValue(tester, 'video-session-camera'), isFalse);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('turning the child confirmation off asks first', (tester) async {
      _expand(tester);
      final api = _FakeVideoSessionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-consent')),
      );
      await tester.pumpAndSettle();
      expect(find.text(VideoSessionCopy.consentOffConfirmBody), findsOneWidget);

      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-consent-cancel')),
      );
      await tester.pumpAndSettle();
      expect(_switchValue(tester, 'video-session-consent'), isTrue);

      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-consent')),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-consent-confirm')),
      );
      await tester.pumpAndSettle();
      expect(_switchValue(tester, 'video-session-consent'), isFalse);
    });

    testWidgets('changing the ring window stays inside the allowed range', (
      tester,
    ) async {
      _expand(tester);
      final api = _FakeVideoSessionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.seconds(30)), findsOneWidget);
      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-ring-increase')),
      );
      await tester.pumpAndSettle();
      expect(find.text(VideoSessionCopy.seconds(35)), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey<String>('video-session-save')));
      await tester.pumpAndSettle();
      expect(api.saved.single.ringTimeoutSeconds, 35);
    });

    testWidgets('a short call ceiling is offered as a choice', (tester) async {
      _expand(tester);
      final api = _FakeVideoSessionApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const ValueKey<String>('video-session-duration-10')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('video-session-save')));
      await tester.pumpAndSettle();

      expect(api.saved.single.maxSessionSeconds, 600);
    });
  });

  group('设备状态', () {
    testWidgets('names the device and its reported permissions', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi()));
      await tester.pumpAndSettle();

      expect(find.text('初芽 · 客厅'), findsOneWidget);
      expect(find.text(VideoSessionCopy.deviceReady), findsOneWidget);
      expect(find.text(VideoSessionCopy.cameraOn), findsOneWidget);
      expect(find.text(VideoSessionCopy.microphoneOff), findsOneWidget);
      expect(find.text(VideoSessionCopy.callIdle), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('says plainly when a device runs an older setting', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi(outdated: true)));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.deviceOutdated), findsOneWidget);
    });

    testWidgets('explains a device that cannot make video calls', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(
        _page(api: _FakeVideoSessionApi(unsupported: true)),
      );
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.deviceUnsupported), findsOneWidget);
    });

    testWidgets('explains what an active call means', (tester) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi(active: true)));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.callActive), findsOneWidget);
    });

    testWidgets('an empty device list says why and what happens next', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(_page(api: _FakeVideoSessionApi(noDevices: true)));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey<String>('video-session-devices-empty')),
        findsOneWidget,
      );
      expect(find.text(VideoSessionCopy.deviceEmptyBody), findsOneWidget);
      // The policy stays editable even with nothing reporting yet.
      expect(
        find.byKey(const ValueKey<String>('video-session-save')),
        findsOneWidget,
      );
    });

    testWidgets('a failed device read leaves the settings readable', (
      tester,
    ) async {
      _expand(tester);
      final api = _FakeVideoSessionApi(
        devicesFailure: _offline(),
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey<String>('video-session-devices-error')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey<String>('video-session-save')),
        findsOneWidget,
      );

      api.devicesFailure = null;
      await tester.tap(
        find.text(VideoSessionCopy.devicesRetryButton),
      );
      await tester.pumpAndSettle();
      expect(find.text('初芽 · 客厅'), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('switching devices reads the same rule under another name', (
      tester,
    ) async {
      _expand(tester);
      await tester.pumpWidget(
        _page(
          api: _FakeVideoSessionApi(twoDevices: true),
          devices: _twoDevices,
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('初芽 · 客厅'), findsOneWidget);
      expect(find.text('初芽 · 卧室'), findsOneWidget);
      // Both rows come from the same family policy, so the rule is shown once.
      expect(find.text(VideoSessionCopy.policyTitle), findsOneWidget);
    });
  });

  group('错误', () {
    testWidgets('a lapsed sign-in asks the guardian to sign in again', (
      tester,
    ) async {
      await tester.pumpWidget(
        _page(api: _FakeVideoSessionApi(policyFailure: _status(401))),
      );
      await tester.pumpAndSettle();

      expect(find.text('登录已过期，请重新登录'), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('an unreachable network offers a retry that works', (
      tester,
    ) async {
      final api = _FakeVideoSessionApi(policyFailure: _offline());
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
      api.policyFailure = null;
      await tester.tap(find.text(VideoSessionCopy.retryButton));
      await tester.pumpAndSettle();

      expect(find.text(VideoSessionCopy.retryButton), findsNothing);
      expect(find.text(VideoSessionCopy.policyTitle), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('a family that never resolves is reported, not spun forever', (
      tester,
    ) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            videoSessionFamilyIdProvider.overrideWith(
              (ref) async => throw mapApiError(_offline()),
            ),
          ],
          child: MaterialApp(
            theme: AppTheme.light,
            home: const VideoSessionPage(),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('video-session-loading')),
        findsNothing,
      );
    });
  });

  group('文案', () {
    testWidgets('never shows a transport term or a raw value', (tester) async {
      _expand(tester);
      await tester.pumpWidget(
        _page(api: _FakeVideoSessionApi(twoDevices: true)),
      );
      await tester.pumpAndSettle();

      expect(_findLeakedTerms(tester), isNull);
      final visible = _visibleText(tester).join('\n');
      for (final banned in <String>['开发中', '检查网络', '一键', '专属', '立刻']) {
        expect(visible.contains(banned), isFalse, reason: banned);
      }
    });

    test('a duration reads as minutes once it passes a minute', () {
      expect(VideoSessionCopy.seconds(30), '30 秒');
      expect(VideoSessionCopy.seconds(120), '2 分钟');
      expect(VideoSessionCopy.seconds(90), '1 分 30 秒');
    });
  });
}

/// The widget tree every case starts from: the real page, the real notifier,
/// and a fake only at the data boundary.
Widget _page({
  required _FakeVideoSessionApi api,
  List<BoundDevice>? devices,
}) {
  final router = GoRouter(
    initialLocation: '/me/video-session',
    routes: <RouteBase>[
      GoRoute(
        path: '/me',
        builder: (context, state) => const Scaffold(body: Text('我的占位')),
      ),
      GoRoute(
        path: '/me/video-session',
        builder: (context, state) => const VideoSessionPage(),
      ),
    ],
  );
  return ProviderScope(
    overrides: [
      // The family-scoped provider must be replaced: left alone it would await
      // a signed-in session that a widget test never has, and the screen would
      // never settle.
      videoSessionFamilyIdProvider.overrideWith((ref) async => 'family-1'),
      videoSessionApiProvider.overrideWith((ref) async => api),
      familyControllerProvider.overrideWith(_SeededFamilyController.new),
      deviceBindingControllerProvider.overrideWith(
        () => _SeededDeviceController(devices ?? _twoDevices),
      ),
      // The device row reads the live capability, which needs its own device
      // session. Left unoverridden the row would silently render nothing, and
      // the capability sentence below would never be exercised.
      videoCapabilityProvider.overrideWith(
        (ref, deviceId) async => _capabilityFor(deviceId),
      ),
    ],
    child: MaterialApp.router(theme: AppTheme.light, routerConfig: router),
  );
}

final _twoDevices = <BoundDevice>[
  _device('device-1', '初芽 · 客厅'),
  _device('device-2', '初芽 · 卧室'),
];

/// Every device reports video support unless a test names it unsupported.
VideoCapability _capabilityFor(String deviceId) {
  return VideoCapability(
    supported: !_unsupportedDevices.contains(deviceId),
    callState: 'idle',
    quality: 'good',
    cameraAllowed: true,
    microphoneAllowed: false,
  );
}

/// Devices the capability override reports as unable to hold a call.
final _unsupportedDevices = <String>{};

BoundDevice _device(String deviceId, String name) {
  return BoundDevice(
    deviceId: deviceId,
    deviceName: name,
    hardwareModel: 'sprout-1',
    firmwareVersion: '1.0.0',
    capabilities: const <String>['camera', 'microphone'],
    lifecycleStatus: 'active',
    boundAt: DateTime.utc(2026, 10, 1),
    runtime: null,
    canManage: true,
  );
}

VideoSessionPolicy _policy() {
  return const VideoSessionPolicy(
    enabled: true,
    cameraAllowed: true,
    microphoneAllowed: false,
    requireChildConsent: true,
    qualityFloor: VideoQualityFloor.fair,
    ringTimeoutSeconds: 30,
    maxSessionSeconds: 1800,
    revision: 3,
  );
}

VideoSessionDevice _deviceStatus({
  String deviceId = 'device-1',
  bool outdated = false,
  bool unsupported = false,
  bool active = false,
}) {
  return VideoSessionDevice(
    deviceId: deviceId,
    sessionSupported: !unsupported,
    cameraAllowed: !unsupported,
    microphoneAllowed: false,
    callState: active ? VideoCallState.active : VideoCallState.idle,
    quality: 'good',
    lastEndReason: VideoEndReason.none,
    upToDate: !outdated,
    reportedAt: DateTime.utc(2026, 10, 22, 7, 4),
  );
}

class _FakeVideoSessionApi implements VideoSessionApi {
  _FakeVideoSessionApi({
    this.pending = false,
    this.refuseSave = false,
    this.noDevices = false,
    this.outdated = false,
    this.unsupported = false,
    this.active = false,
    this.twoDevices = false,
    this.policyFailure,
    this.devicesFailure,
  });

  final bool pending;
  final bool refuseSave;
  final bool noDevices;
  final bool outdated;
  final bool unsupported;
  final bool active;
  final bool twoDevices;
  Object? policyFailure;
  Object? devicesFailure;

  final List<VideoSessionPolicy> saved = <VideoSessionPolicy>[];
  int? lastSavedRevision;
  VideoSessionPolicy _stored = _policy();
  Completer<void>? _gate;
  bool _isReleased = false;

  /// Holds the first read open until the test releases it.
  ///
  /// The release may arrive before the read has created its gate — the widget
  /// only starts reading on the first frame after mounting — so the released
  /// state is remembered instead of depending on the gate already existing.
  void release() {
    _isReleased = true;
    _gate?.complete();
    _gate = null;
  }

  Future<void> _awaitGate() async {
    if (!pending || _isReleased) {
      return;
    }
    _gate ??= Completer<void>();
    await _gate!.future;
  }

  @override
  Future<VideoSessionSnapshot> fetchPolicy() async {
    await _awaitGate();
    final failure = policyFailure;
    if (failure != null) {
      throw mapApiError(failure);
    }
    return VideoSessionSnapshot(
      policy: _stored,
      qualities: const <VideoQualityFloor>[
        VideoQualityFloor.good,
        VideoQualityFloor.fair,
        VideoQualityFloor.poor,
      ],
    );
  }

  @override
  Future<VideoSessionPolicy> publishPolicy(VideoSessionPolicy policy) async {
    if (refuseSave) {
      throw const VideoSessionRefused(VideoSessionRefusal.invalidPolicy);
    }
    saved.add(policy);
    _stored = policy.copyWith(revision: _stored.revision + 1);
    lastSavedRevision = _stored.revision;
    return _stored;
  }

  @override
  Future<List<VideoSessionDevice>> fetchDevices() async {
    final failure = devicesFailure;
    if (failure != null) {
      throw mapApiError(failure);
    }
    if (noDevices) {
      return const <VideoSessionDevice>[];
    }
    if (twoDevices) {
      return <VideoSessionDevice>[
        _deviceStatus(),
        _deviceStatus(deviceId: 'device-2', outdated: true),
      ];
    }
    return <VideoSessionDevice>[
      _deviceStatus(outdated: outdated, unsupported: unsupported, active: active),
    ];
  }
}

class _SeededFamilyController extends FamilyController {
  @override
  Future<FamilyMembershipState> build() async {
    return const FamilyMembershipState(
      capability: FamilyCapability(
        familyId: 'family-1',
        ownerUserId: 'user-1',
        memberCount: 1,
        pendingInviteCount: 0,
        schemaVersion: '1',
      ),
      members: <FamilyMember>[],
      invitations: <FamilyInvitation>[],
    );
  }
}

class _SeededDeviceController extends DeviceBindingController {
  _SeededDeviceController(this._devices);

  final List<BoundDevice> _devices;

  @override
  Future<DeviceBindingState> build() async {
    return DeviceBindingState(
      isScanning: false,
      devices: const <DiscoveredDevice>[],
      bindings: _devices,
    );
  }
}

bool? _switchValue(WidgetTester tester, String key) {
  final tile = tester.widget<SwitchListTile>(
    find.byKey(ValueKey<String>(key)),
  );
  return tile.value;
}

/// Gives the page a tall viewport before a case reaches below the fold.
void _expand(WidgetTester tester) {
  tester.view.physicalSize = const Size(1200, 3600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

List<String> _visibleText(WidgetTester tester) {
  return tester
      .widgetList<Text>(find.byType(Text))
      .map((text) => text.data ?? text.textSpan?.toPlainText() ?? '')
      .toList(growable: false);
}

String? _findLeakedTerms(WidgetTester tester) {
  const banned = <String>[
    'API',
    'token',
    'Token',
    'session',
    'status',
    'camera_allowed',
    '127.0.0.1',
    '[object Object]',
    'undefined',
    'null',
    'NaN',
    'Exception',
    '检查网络配置',
  ];
  for (final text in _visibleText(tester)) {
    for (final word in banned) {
      if (text.contains(word)) {
        return word;
      }
    }
  }
  return null;
}

/// A connection that never reaches the service, as the transport reports it.
DioException _offline() {
  return DioException(
    requestOptions: RequestOptions(path: '/api/v1/video-session/policy'),
    type: DioExceptionType.connectionError,
    message: 'SocketException: connection refused (127.0.0.1:8081)',
  );
}

/// A response with a status and a code, exactly as the transport reports it.
DioException _status(int status, {String code = 'unauthenticated'}) {
  final options = RequestOptions(path: '/api/v1/video-session/policy');
  return DioException(
    requestOptions: options,
    type: DioExceptionType.badResponse,
    response: Response<Object?>(
      requestOptions: options,
      statusCode: status,
      data: <String, Object?>{
        'error': <String, Object?>{
          'code': code,
          'message': 'raw backend message',
          'retryable': false,
        },
      },
    ),
  );
}
