import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/power/application/power_controller.dart';
import 'package:parent_app/features/power/domain/power_policy.dart';
import 'package:parent_app/features/power/presentation/battery_settings_page.dart';

void main() {
  testWidgets('shows the saved battery policy values', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          powerControllerProvider.overrideWith(
            () => _FakePowerController(
              const PowerPolicy(
                enabled: true,
                lowPowerEnabled: true,
                deepSleepEnabled: true,
                chargePriority: true,
                lowBatteryWarnPercent: 30,
                lowPowerEnterPercent: 20,
                sleepAfterSeconds: 900,
                revision: 3,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: BatterySettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('电池与省电'), findsOneWidget);
    expect(find.text('启用省电策略'), findsOneWidget);
    expect(find.text('30 %'), findsOneWidget);
    expect(find.text('20 %'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('深度休眠'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('深度休眠'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('保存电源设置'),
      400,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('保存电源设置'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('saves an edited policy through the controller', (tester) async {
    final controller = _FakePowerController(
      const PowerPolicy(
        enabled: true,
        lowPowerEnabled: true,
        deepSleepEnabled: true,
        chargePriority: true,
        lowBatteryWarnPercent: 20,
        lowPowerEnterPercent: 15,
        sleepAfterSeconds: 1800,
        revision: 1,
      ),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [powerControllerProvider.overrideWith(() => controller)],
        child: const MaterialApp(home: BatterySettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(
      find.text('保存电源设置'),
      600,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存电源设置'));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.lowPowerEnterPercent, 15);
  });
}

class _FakePowerController extends PowerController {
  _FakePowerController(this.value);

  final PowerPolicy value;
  PowerPolicy? saved;

  @override
  Future<PowerPolicy> build() async => value;

  @override
  Future<PowerPolicy> save(PowerPolicy policy) async {
    saved = policy;
    return policy;
  }
}
