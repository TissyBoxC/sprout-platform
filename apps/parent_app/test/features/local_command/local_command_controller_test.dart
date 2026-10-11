import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/local_command/application/local_command_controller.dart';
import 'package:parent_app/features/local_command/data/local_command_api.dart';
import 'package:parent_app/features/local_command/domain/local_command.dart';

void main() {
  test('loads the published set on first build', () async {
    final api = _FakeLocalCommandApi(
      set: _setWith([
        _command('cmd_play', '听故事', LocalCommandAction.playPackage, 'story_001'),
      ]),
    );
    final container = ProviderContainer(
      overrides: [localCommandApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final set = await container.read(localCommandControllerProvider.future);

    expect(set.revision, 4);
    expect(set.commands, hasLength(1));
    expect(set.commands.single.phrase, '听故事');
  });

  test('save replaces the whole set and updates the revision', () async {
    final api = _FakeLocalCommandApi(set: const LocalCommandSet.empty());
    final container = ProviderContainer(
      overrides: [localCommandApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(localCommandControllerProvider.future);
    await container.read(localCommandControllerProvider.notifier).save([
      _command('cmd_stop', '停下', LocalCommandAction.stop, ''),
    ]);

    final set = container.read(localCommandControllerProvider).value!;
    expect(set.revision, 1);
    expect(set.commands.single.commandId, 'cmd_stop');
  });

  test('toggleEnabled flips one command without touching the others',
      () async {
    final api = _FakeLocalCommandApi(
      set: _setWith([
        _command('a', '口令一', LocalCommandAction.stop, '', enabled: true),
        _command('b', '口令二', LocalCommandAction.pause, '', enabled: true),
      ]),
    );
    final container = ProviderContainer(
      overrides: [localCommandApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(localCommandControllerProvider.future);
    await container
        .read(localCommandControllerProvider.notifier)
        .toggleEnabled('b');

    final set = container.read(localCommandControllerProvider).value!;
    expect(set.commands.firstWhere((c) => c.commandId == 'a').enabled, isTrue);
    expect(set.commands.firstWhere((c) => c.commandId == 'b').enabled, isFalse);
  });

  test('delete removes one command and refreshes', () async {
    final api = _FakeLocalCommandApi(
      set: _setWith([
        _command('cmd_stop', '停下', LocalCommandAction.stop, ''),
      ]),
    );
    final container = ProviderContainer(
      overrides: [localCommandApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(localCommandControllerProvider.future);
    await container
        .read(localCommandControllerProvider.notifier)
        .delete('cmd_stop');

    expect(api.deleted, <String>['cmd_stop']);
    expect(container.read(localCommandControllerProvider).value!.commands, isEmpty);
  });
}

LocalCommand _command(
  String id,
  String phrase,
  LocalCommandAction action,
  String argument, {
  bool enabled = true,
}) {
  return LocalCommand(
    commandId: id,
    phrase: phrase,
    action: action,
    argument: argument,
    enabled: enabled,
    revision: 4,
  );
}

LocalCommandSet _setWith(List<LocalCommand> commands) {
  return LocalCommandSet(revision: 4, commands: commands);
}

class _FakeLocalCommandApi implements LocalCommandApi {
  _FakeLocalCommandApi({required this.set});

  LocalCommandSet set;
  final List<String> deleted = <String>[];
  int _revision = 0;

  @override
  Future<LocalCommandSet> fetchSet() async => set;

  @override
  Future<LocalCommandSet> replaceSet(List<LocalCommand> commands) async {
    _revision += 1;
    set = LocalCommandSet(revision: _revision, commands: commands);
    return set;
  }

  @override
  Future<void> deleteCommand(String commandId) async {
    deleted.add(commandId);
    set = LocalCommandSet(
      revision: set.revision,
      commands: [
        for (final command in set.commands)
          if (command.commandId != commandId) command,
      ],
    );
  }
}
