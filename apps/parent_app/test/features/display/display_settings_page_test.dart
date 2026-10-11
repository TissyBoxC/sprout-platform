import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/display/application/display_controller.dart';
import 'package:parent_app/features/display/domain/display_policy.dart';
import 'package:parent_app/features/display/presentation/display_settings_page.dart';

void main() {
  testWidgets('shows the saved eye-care policy values', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          displayControllerProvider.overrideWith(
            () => _FakeDisplayController(
              const DisplayPolicy(
                brightnessPercent: 75,
                colorTemperatureKelvin: 3200,
                minViewDistanceCm: 35,
                dailyLimitMinutes: 90,
                breakIntervalMinutes: 15,
                dimAfterSeconds: 45,
                autoBrightness: true,
                nightLight: true,
                distanceReminder: true,
                revision: 4,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: DisplaySettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('护眼屏幕'), findsOneWidget);
    expect(find.text('屏幕亮度'), findsOneWidget);
    expect(find.text('75 %'), findsOneWidget);
    expect(find.text('3200 K'), findsOneWidget);
    expect(find.text('90 分钟'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('自动亮度'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('自动亮度'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('保存护眼设置'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('保存护眼设置'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('saves an edited policy through the controller', (tester) async {
    final controller = _FakeDisplayController(
      const DisplayPolicy(
        brightnessPercent: 60,
        colorTemperatureKelvin: 4500,
        minViewDistanceCm: 40,
        dailyLimitMinutes: 60,
        breakIntervalMinutes: 20,
        dimAfterSeconds: 60,
        autoBrightness: true,
        nightLight: true,
        distanceReminder: true,
        revision: 1,
      ),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          displayControllerProvider.overrideWith(() => controller),
        ],
        child: const MaterialApp(home: DisplaySettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(
      find.text('保存护眼设置'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(find.text('保存护眼设置'));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.brightnessPercent, 60);
  });
}

class _FakeDisplayController extends DisplayController {
  _FakeDisplayController(this.value);

  final DisplayPolicy value;
  DisplayPolicy? saved;

  @override
  Future<DisplayPolicy> build() async => value;

  @override
  Future<DisplayPolicy> save(DisplayPolicy policy) async {
    saved = policy;
    return policy;
  }
}
