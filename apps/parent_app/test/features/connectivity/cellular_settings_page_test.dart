import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/connectivity/application/cellular_controller.dart';
import 'package:parent_app/features/connectivity/domain/cellular_policy.dart';
import 'package:parent_app/features/connectivity/presentation/cellular_settings_page.dart';

void main() {
  testWidgets('shows the saved cellular policy values', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          cellularControllerProvider.overrideWith(
            () => _FakeCellularController(
              const CellularPolicy(
                enabled: true,
                roamingAllowed: true,
                remoteWakeEnabled: true,
                meteredUploadLimit: false,
                preference: RadioPreference.cellularFirst,
                monthlyQuotaMb: 4096,
                lowQuotaWarnPercent: 25,
                keepaliveSeconds: 600,
                revision: 3,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: CellularSettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('蜂窝网络'), findsOneWidget);
    expect(find.text('启用蜂窝网络'), findsOneWidget);
    expect(find.text('4096 MB'), findsOneWidget);
    expect(find.text('25 %'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('允许漫游'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('允许漫游'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('保存蜂窝网络设置'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('保存蜂窝网络设置'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('saves an edited policy through the controller', (tester) async {
    final controller = _FakeCellularController(
      const CellularPolicy(
        enabled: true,
        roamingAllowed: false,
        remoteWakeEnabled: true,
        meteredUploadLimit: true,
        preference: RadioPreference.wifiFirst,
        monthlyQuotaMb: 2048,
        lowQuotaWarnPercent: 20,
        keepaliveSeconds: 300,
        revision: 1,
      ),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cellularControllerProvider.overrideWith(() => controller)],
        child: const MaterialApp(home: CellularSettingsPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(
      find.text('保存蜂窝网络设置'),
      600,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存蜂窝网络设置'));
    await tester.pumpAndSettle();

    expect(controller.saved, isNotNull);
    expect(controller.saved!.preference, RadioPreference.wifiFirst);
  });
}

class _FakeCellularController extends CellularController {
  _FakeCellularController(this.value);

  final CellularPolicy value;
  CellularPolicy? saved;

  @override
  Future<CellularPolicy> build() async => value;

  @override
  Future<CellularPolicy> save(CellularPolicy policy) async {
    saved = policy;
    return policy;
  }
}
