import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/touch/application/touch_controller.dart';
import 'package:parent_app/features/touch/domain/touch_config.dart';
import 'package:parent_app/features/touch/presentation/touch_settings_page.dart';

void main() {
  testWidgets('shows the saved touch configuration values', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          touchControllerProvider.overrideWith(
            () => _FakeTouchController(
              const TouchConfig(
                enabled: true,
                tapEnabled: true,
                swipeEnabled: true,
                longPressEnabled: true,
                hapticEnabled: true,
                sensitivity: 75,
                rotationDegrees: 90,
                longPressMs: 600,
                doubleTapMs: 250,
                swipeDistancePx: 80,
                revision: 3,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: TouchSettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('触摸交互'), findsOneWidget);
    expect(find.text('触摸灵敏度'), findsOneWidget);
    expect(find.text('75 %'), findsOneWidget);
    expect(find.text('600 毫秒'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('90°'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('90°'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('触觉反馈'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('触觉反馈'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('保存触摸设置'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('保存触摸设置'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('saves an edited configuration through the controller', (
    tester,
  ) async {
    final controller = _FakeTouchController(
      const TouchConfig(
        enabled: true,
        tapEnabled: true,
        swipeEnabled: true,
        longPressEnabled: true,
        hapticEnabled: true,
        sensitivity: 60,
        rotationDegrees: 0,
        longPressMs: 700,
        doubleTapMs: 300,
        swipeDistancePx: 60,
        revision: 1,
      ),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          touchControllerProvider.overrideWith(() => controller),
        ],
        child: const MaterialApp(home: TouchSettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(
      find.text('保存触摸设置'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(find.text('保存触摸设置'));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.sensitivity, 60);
  });
}

class _FakeTouchController extends TouchController {
  _FakeTouchController(this.value);

  final TouchConfig value;
  TouchConfig? saved;

  @override
  Future<TouchConfig> build() async => value;

  @override
  Future<TouchConfig> save(TouchConfig config) async {
    saved = config;
    return config;
  }
}
