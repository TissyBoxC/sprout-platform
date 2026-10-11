import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/offline_policy/application/offline_policy_controller.dart';
import 'package:parent_app/features/offline_policy/domain/offline_policy.dart';
import 'package:parent_app/features/offline_policy/presentation/offline_policy_page.dart';

void main() {
  testWidgets('shows the published offline policy summary and details',
      (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          offlinePolicyControllerProvider.overrideWith(
            () => _FakeOfflinePolicyController(
              const OfflinePolicy(
                offlineEnabled: true,
                maxCachedPackages: 25,
                maxCacheBytes: 268435456,
                cacheTtlHours: 72,
                downloadOverWifiOnly: true,
                revision: 7,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: OfflinePolicyPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('离线下载'), findsOneWidget);
    expect(find.text('已开启离线播放'), findsOneWidget);
    expect(find.text('25 个'), findsOneWidget);
    expect(find.text('256 MB'), findsOneWidget);
    expect(find.text('72 小时'), findsOneWidget);
    expect(find.text('仅 Wi-Fi'), findsOneWidget);
    expect(find.text('当前策略版本 7'), findsOneWidget);
  });

  testWidgets('shows the disabled offline state', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          offlinePolicyControllerProvider.overrideWith(
            () => _FakeOfflinePolicyController(
              const OfflinePolicy(
                offlineEnabled: false,
                maxCachedPackages: 5,
                maxCacheBytes: 16777216,
                cacheTtlHours: 6,
                downloadOverWifiOnly: false,
                revision: 2,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: OfflinePolicyPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('离线播放已关闭'), findsOneWidget);
    expect(find.text('Wi-Fi 与移动网络'), findsOneWidget);
  });
}

class _FakeOfflinePolicyController extends OfflinePolicyController {
  _FakeOfflinePolicyController(this.value);

  final OfflinePolicy value;

  @override
  Future<OfflinePolicy> build() async => value;
}
