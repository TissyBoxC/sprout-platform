import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/local_command/application/local_command_controller.dart';
import 'package:parent_app/features/local_command/domain/local_command.dart';
import 'package:parent_app/features/local_command/presentation/local_command_page.dart';

void main() {
  testWidgets('shows the branded empty state with no commands', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          localCommandControllerProvider.overrideWith(
            () => _FakeLocalCommandController(const LocalCommandSet.empty()),
          ),
        ],
        child: const MaterialApp(home: LocalCommandPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('本地指令'), findsOneWidget);
    expect(find.text('还没有本地指令'), findsOneWidget);
  });

  testWidgets('lists commands with action, argument, and revision',
      (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          localCommandControllerProvider.overrideWith(
            () => _FakeLocalCommandController(
              const LocalCommandSet(
                revision: 5,
                commands: [
                  LocalCommand(
                    commandId: 'cmd_play',
                    phrase: '听故事',
                    action: LocalCommandAction.playPackage,
                    argument: 'story_001',
                    enabled: true,
                    revision: 5,
                  ),
                ],
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: LocalCommandPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('听故事'), findsOneWidget);
    expect(find.text('播放内容 · story_001'), findsOneWidget);
    expect(find.text('已发布到第 5 版'), findsOneWidget);
  });
}

class _FakeLocalCommandController extends LocalCommandController {
  _FakeLocalCommandController(this.value);

  final LocalCommandSet value;

  @override
  Future<LocalCommandSet> build() async => value;
}
