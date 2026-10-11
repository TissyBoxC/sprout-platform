import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/form_factor/application/form_factor_controller.dart';
import 'package:parent_app/features/form_factor/data/form_factor_api.dart';
import 'package:parent_app/features/form_factor/domain/form_factor_profile.dart';
import 'package:parent_app/features/form_factor/presentation/form_factor_page.dart';

void main() {
  testWidgets('shows the composed capabilities and actions', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          formFactorApiProvider.overrideWithValue(_FakeFormFactorApi()),
        ],
        child: const MaterialApp(home: FormFactorPage(deviceId: 'device-1')),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('形态能力'), findsOneWidget);
    expect(find.text('可移动形态'), findsOneWidget);
    expect(find.text('技术名称：moving_body'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('行走能力'),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('行走能力'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('可用动作'),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('尾巴动作'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('shows an empty state for an unreported device', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          formFactorApiProvider.overrideWithValue(
            _FakeFormFactorApi(supported: false),
          ),
        ],
        child: const MaterialApp(home: FormFactorPage(deviceId: 'device-2')),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('设备尚未上报形态能力。'), findsOneWidget);
  });
}

class _FakeFormFactorApi implements FormFactorApi {
  _FakeFormFactorApi({this.supported = true});

  final bool supported;

  @override
  Future<FormFactorCapability> fetchCapability(String deviceId) async {
    if (!supported) {
      return const FormFactorCapability(
        supported: false,
        technicalName: '',
        capabilityMask: 0,
        actionMask: 0,
        capabilities: <FormFactorBit>[],
        actions: <FormFactorBit>[],
        schemaVersion: 1,
      );
    }
    return const FormFactorCapability(
      supported: true,
      technicalName: 'moving_body',
      capabilityMask: 97,
      actionMask: 248,
      capabilities: <FormFactorBit>[
        FormFactorBit(key: 'display', name: 'display'),
        FormFactorBit(key: 'locomotion', name: 'locomotion'),
        FormFactorBit(key: 'servo', name: 'servo'),
      ],
      actions: <FormFactorBit>[
        FormFactorBit(key: 'expression', name: 'expression'),
        FormFactorBit(key: 'walk', name: 'walk'),
        FormFactorBit(key: 'tail', name: 'tail'),
      ],
      schemaVersion: 1,
    );
  }
}
