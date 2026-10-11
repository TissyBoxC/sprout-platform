import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/motion/application/motion_controller.dart';
import 'package:parent_app/features/motion/domain/motion_policy.dart';
import 'package:parent_app/features/motion/presentation/motion_settings_page.dart';

void main() {
  testWidgets('shows the saved motion policy values', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          motionControllerProvider.overrideWith(
            () => _FakeMotionController(
              const MotionPolicy(
                enabled: true,
                requireStopAck: true,
                allowLocomotion: true,
                maxSpeedPercent: 80,
                maxRuntimeMs: 4000,
                stopGraceMs: 250,
                revision: 3,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: MotionSettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('动作与电机'), findsOneWidget);
    expect(find.text('启用动作控制'), findsOneWidget);
    expect(find.text('80 %'), findsOneWidget);
    expect(find.text('4000 毫秒'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('允许行走与舞蹈'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('允许行走与舞蹈'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('保存动作设置'),
      400,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('保存动作设置'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('saves an edited policy through the controller', (tester) async {
    final controller = _FakeMotionController(
      const MotionPolicy(
        enabled: true,
        requireStopAck: true,
        allowLocomotion: false,
        maxSpeedPercent: 60,
        maxRuntimeMs: 5000,
        stopGraceMs: 300,
        revision: 1,
      ),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [motionControllerProvider.overrideWith(() => controller)],
        child: const MaterialApp(home: MotionSettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(
      find.text('保存动作设置'),
      600,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存动作设置'));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.maxSpeedPercent, 60);
  });
}

class _FakeMotionController extends MotionController {
  _FakeMotionController(this.value);

  final MotionPolicy value;
  MotionPolicy? saved;

  @override
  Future<MotionPolicy> build() async => value;

  @override
  Future<MotionPolicy> save(MotionPolicy policy) async {
    saved = policy;
    return policy;
  }
}
