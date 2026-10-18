import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:esp_provisioning_wifi/esp_provisioning_wifi.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/features/device/domain/device_payload.dart';
import 'package:parent_app/features/device/presentation/device_qr_scan_page.dart';
import 'package:parent_app/features/device/presentation/device_provisioning_page.dart';

void main() {
  testWidgets('scan page exposes a return control without a camera', (
    tester,
  ) async {
    final observer = _NavigationObserver();
    await tester.pumpWidget(
      ProviderScope(
        child: MaterialApp(
          navigatorObservers: [observer],
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => DeviceQrScanPage(
                        scannerViewBuilder: (_, _) =>
                            const ColoredBox(color: Colors.black),
                      ),
                    ),
                  ),
                  child: const Text('打开扫码页'),
                ),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开扫码页'));
    await tester.pumpAndSettle();

    expect(find.byTooltip('返回'), findsOneWidget);
    expect(find.text('扫描设备绑定码'), findsOneWidget);

    await tester.tap(find.byTooltip('返回'));
    await tester.pumpAndSettle();

    expect(find.text('打开扫码页'), findsOneWidget);
    expect(observer.popCount, 1);
  });

  testWidgets('system back uses the same cleanup path', (tester) async {
    final observer = _NavigationObserver();
    await tester.pumpWidget(
      ProviderScope(
        child: MaterialApp(
          navigatorObservers: [observer],
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => DeviceQrScanPage(
                        scannerViewBuilder: (_, _) =>
                            const ColoredBox(color: Colors.black),
                      ),
                    ),
                  ),
                  child: const Text('打开扫码页'),
                ),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开扫码页'));
    await tester.pumpAndSettle();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();

    expect(find.text('打开扫码页'), findsOneWidget);
    expect(observer.popCount, 1);
  });

  testWidgets('successful provisioning refreshes devices and opens the list', (
    tester,
  ) async {
    final controller = _FlowDeviceBindingController();
    final router = GoRouter(
      initialLocation: '/devices/scan',
      routes: [
        GoRoute(
          path: '/devices/scan',
          builder: (context, state) => DeviceQrScanPage(
            scannerViewBuilder: (_, _) => const ColoredBox(color: Colors.black),
            initialPayload: 'sprout://setup?service=SPROUT-DEMO&pop=proof',
          ),
        ),
        GoRoute(
          path: '/devices/provision',
          builder: (context, state) =>
              DeviceProvisioningPage(setup: state.extra! as DeviceSetupPayload),
        ),
        GoRoute(
          path: '/devices',
          builder: (context, state) =>
              const Scaffold(body: Center(child: Text('设备列表'))),
        ),
      ],
    );
    addTearDown(router.dispose);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          deviceBindingControllerProvider.overrideWith(() => controller),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));

    expect(find.text('正在寻找初芽'), findsOneWidget);
    expect(find.text('连接新设备'), findsOneWidget);
    expect(find.text('SPROUT-DEMO'), findsOneWidget);
    expect(controller.refreshCalls, 0);

    await tester.tap(find.text('SPROUT-DEMO'));
    await tester.pump(const Duration(milliseconds: 500));
    await tester.tap(find.text('Home'));
    await tester.pump(const Duration(milliseconds: 500));
    await tester.tap(find.text('连接无线网络'));
    await tester.pump(const Duration(milliseconds: 500));
    await tester.tap(find.text('查看我的设备'));
    await _pumpUntilFound(tester, find.text('设备列表'));

    expect(find.text('设备列表'), findsOneWidget);
    expect(controller.refreshCalls, greaterThanOrEqualTo(1));
  });
}

Future<void> _pumpUntilFound(
  WidgetTester tester,
  Finder finder, {
  Duration timeout = const Duration(seconds: 5),
}) async {
  final deadline = DateTime.now().add(timeout);
  while (finder.evaluate().isEmpty && DateTime.now().isBefore(deadline)) {
    await tester.pump(const Duration(milliseconds: 50));
  }
}

class _FlowDeviceBindingController extends DeviceBindingController {
  int refreshCalls = 0;

  @override
  Future<DeviceBindingState> build() async {
    return const DeviceBindingState(
      isScanning: false,
      devices: [],
      bindings: [],
    );
  }

  @override
  Future<void> refresh() async {
    refreshCalls += 1;
  }

  @override
  Future<List<String>> scanProvisioningDevices() async => const ['SPROUT-DEMO'];

  @override
  Future<List<EspWifiNetwork>> scanWifiNetworks(
    DeviceSetupPayload setup,
  ) async {
    return const [
      EspWifiNetwork(
        ssid: 'Home',
        rssi: -40,
        security: EspWifiSecurity.wpa2Psk,
      ),
    ];
  }

  @override
  Future<void> provisionWifi({
    required DeviceSetupPayload setup,
    required String ssid,
    required String password,
  }) async {}

  @override
  Future<BoundDevice> bindProvisionedDevice({
    required DeviceSetupPayload setup,
  }) async {
    return _boundDevice;
  }

  @override
  Future<void> cancelProvisioning() async {}
}

final _boundDevice = BoundDevice(
  deviceId: 'device-001',
  deviceName: '初芽',
  hardwareModel: 'sprout_initial',
  firmwareVersion: '0.3.0',
  capabilities: const ['display'],
  lifecycleStatus: 'active',
  boundAt: DateTime.parse('2026-10-01T08:00:00Z'),
  runtime: null,
);

class _NavigationObserver extends NavigatorObserver {
  int popCount = 0;

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    popCount += 1;
    super.didPop(route, previousRoute);
  }
}
