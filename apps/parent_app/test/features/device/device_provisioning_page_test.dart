import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/domain/device_payload.dart';
import 'package:parent_app/features/device/presentation/device_provisioning_page.dart';

void main() {
  testWidgets('provisioning page exposes a return control', (tester) async {
    final observer = _NavigationObserver();
    await tester.pumpWidget(
      _buildProvisioningApp(
        const DeviceSetupPayload(
          serviceName: 'SPROUT-DEMO',
          proofOfPossession: 'proof',
          username: 'sprout-provisioning',
        ),
        observer: observer,
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(find.text('打开配网页'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.byTooltip('返回'), findsOneWidget);
    await tester.tap(find.byTooltip('返回'));
    await tester.pumpAndSettle();

    expect(find.text('打开配网页'), findsOneWidget);
    expect(observer.popCount, 1);
  });

  testWidgets('system back uses the provisioning cleanup path', (tester) async {
    final observer = _NavigationObserver();
    await tester.pumpWidget(
      _buildProvisioningApp(
        const DeviceSetupPayload(
          serviceName: 'SPROUT-DEMO',
          proofOfPossession: 'proof',
          username: 'sprout-provisioning',
        ),
        observer: observer,
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(find.text('打开配网页'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    await tester.binding.handlePopRoute();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.text('打开配网页'), findsOneWidget);
    expect(observer.popCount, 1);
  });
}

Widget _buildProvisioningApp(
  DeviceSetupPayload setup, {
  required _NavigationObserver observer,
}) {
  return ProviderScope(
    overrides: [
      deviceBindingControllerProvider.overrideWith(
        _FakeDeviceBindingController.new,
      ),
    ],
    child: MaterialApp(
      navigatorObservers: [observer],
      home: Builder(
        builder: (context) => Scaffold(
          body: Center(
            child: FilledButton(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => DeviceProvisioningPage(setup: setup),
                ),
              ),
              child: const Text('打开配网页'),
            ),
          ),
        ),
      ),
    ),
  );
}

class _FakeDeviceBindingController extends DeviceBindingController {
  @override
  Future<DeviceBindingState> build() async {
    return const DeviceBindingState(
      isScanning: false,
      devices: [],
      bindings: [],
    );
  }

  @override
  Future<List<String>> scanProvisioningDevices() async => const [];

  @override
  Future<void> cancelProvisioning() async {}
}

class _NavigationObserver extends NavigatorObserver {
  int popCount = 0;

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    popCount += 1;
    super.didPop(route, previousRoute);
  }
}
