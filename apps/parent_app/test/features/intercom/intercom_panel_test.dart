import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/intercom/application/intercom_controller.dart';
import 'package:parent_app/features/intercom/data/intercom_api.dart';
import 'package:parent_app/features/intercom/presentation/intercom_panel.dart';

void main() {
  group('IntercomPanel', () {
    testWidgets('idle state explains that the child must accept', (tester) async {
      await tester.pumpWidget(_app(_FakeCallController(null)));
      await tester.pumpAndSettle();

      expect(find.text('语音对讲'), findsOneWidget);
      // The explanation matters: the guardian learns the call is not automatic.
      expect(find.textContaining('孩子按键接受才会接通'), findsOneWidget);
      expect(find.text('呼叫设备'), findsOneWidget);
    });

    testWidgets('ringing state says no audio is being transmitted yet', (
      tester,
    ) async {
      await tester.pumpWidget(
        _app(_FakeCallController(_call(DeviceCallState.ringing))),
      );
      await tester.pumpAndSettle();

      expect(find.text('正在呼叫'), findsOneWidget);
      expect(find.textContaining('现在还没有传输声音'), findsOneWidget);
      expect(find.text('取消呼叫'), findsOneWidget);
      // The microphone indicator must NOT appear while ringing.
      expect(find.byIcon(Icons.mic), findsNothing);
    });

    testWidgets('active state shows a live microphone indicator', (
      tester,
    ) async {
      await tester.pumpWidget(
        _app(_FakeCallController(_call(DeviceCallState.active))),
      );
      await tester.pumpAndSettle();

      expect(find.text('已接通'), findsOneWidget);
      expect(find.byIcon(Icons.mic), findsOneWidget);
      expect(find.textContaining('麦克风都在工作'), findsOneWidget);
      expect(find.text('挂断'), findsOneWidget);
      // Ringing copy must be gone once connected.
      expect(find.textContaining('还没有传输声音'), findsNothing);
    });

    testWidgets('declined state names what happened', (tester) async {
      await tester.pumpWidget(
        _app(_FakeCallController(_call(DeviceCallState.declined))),
      );
      await tester.pumpAndSettle();

      expect(find.text('孩子拒绝了这次呼叫'), findsOneWidget);
      expect(find.byIcon(Icons.mic), findsNothing);
    });

    testWidgets('missed state explains the call ended by itself', (
      tester,
    ) async {
      await tester.pumpWidget(
        _app(_FakeCallController(_call(DeviceCallState.missed))),
      );
      await tester.pumpAndSettle();

      expect(find.text('未接听'), findsOneWidget);
      expect(find.textContaining('没有接听'), findsOneWidget);
    });

    testWidgets('calling the device invokes the controller', (tester) async {
      final controller = _FakeCallController(null);
      await tester.pumpWidget(_app(controller));
      await tester.pumpAndSettle();

      await tester.tap(find.text('呼叫设备'));
      await tester.pumpAndSettle();

      expect(controller.started, isTrue);
    });

    testWidgets('hanging up an active call invokes the controller', (
      tester,
    ) async {
      final controller = _FakeCallController(_call(DeviceCallState.active));
      await tester.pumpWidget(_app(controller));
      await tester.pumpAndSettle();

      await tester.tap(find.text('挂断'));
      await tester.pumpAndSettle();

      expect(controller.hungUp, isTrue);
    });
  });
}

class _FakeCallController extends IntercomCallController {
  _FakeCallController(this._initial) : super('device-1');

  final DeviceCall? _initial;
  bool started = false;
  bool hungUp = false;

  @override
  Future<DeviceCall?> build() async => _initial;

  @override
  Future<DeviceCall> start() async {
    started = true;
    return _call(DeviceCallState.ringing);
  }

  @override
  Future<void> hangUp() async {
    hungUp = true;
  }
}

Widget _app(_FakeCallController controller) {
  return ProviderScope(
    overrides: [
      intercomCallControllerProvider(
        'device-1',
      ).overrideWith(() => controller),
    ],
    child: MaterialApp(
      theme: AppTheme.light,
      home: const Scaffold(
        body: Padding(
          padding: EdgeInsets.all(16),
          child: IntercomPanel(deviceId: 'device-1', deviceName: '初芽'),
        ),
      ),
    ),
  );
}

DeviceCall _call(DeviceCallState state) {
  final now = DateTime.now().toUtc();
  return DeviceCall(
    callId: 'call-1',
    deviceId: 'device-1',
    state: state,
    startedAt: now,
    deadline: now.add(const Duration(seconds: 45)),
    acceptedAt: state == DeviceCallState.active ? now : null,
    finishedAt: state == DeviceCallState.active ? null : now,
    durationSeconds: state == DeviceCallState.ended ? 75 : 0,
  );
}
