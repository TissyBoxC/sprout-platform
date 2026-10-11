import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/local_command_controller.dart';
import '../domain/local_command.dart';

/// Guardian view of offline local commands: publish trigger phrases that map
/// to actions the device performs with no network.
class LocalCommandPage extends ConsumerWidget {
  const LocalCommandPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final set = ref.watch(localCommandControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('本地指令'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _openEditor(context, ref, null),
        icon: const Icon(Icons.add_rounded),
        label: const Text('新增指令'),
      ),
      body: AppStateSwitcher(
        stateKey: set.when(
          data: (value) => value.isEmpty ? 'commands-empty' : 'commands-list',
          error: (_, _) => 'commands-error',
          loading: () => 'commands-loading',
        ),
        child: set.when(
          loading: () => const Center(
            key: ValueKey<String>('commands-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('commands-error'),
            message: LocalCommandController.messageFor(error),
            onRetry: () =>
                ref.read(localCommandControllerProvider.notifier).refresh(),
          ),
          data: (value) => RefreshIndicator(
            key: ValueKey<String>(
              value.isEmpty ? 'commands-empty' : 'commands-list',
            ),
            onRefresh: () =>
                ref.read(localCommandControllerProvider.notifier).refresh(),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
              children: [
                AppReveal(child: _IntroCard(revision: value.revision)),
                const SizedBox(height: 16),
                if (value.isEmpty)
                  const AppReveal(
                    delay: Duration(milliseconds: 60),
                    child: _EmptyCard(),
                  )
                else
                  for (var index = 0; index < value.commands.length; index++)
                    AppReveal(
                      delay: Duration(milliseconds: 45 * index.clamp(0, 6)),
                      child: Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: _CommandCard(command: value.commands[index]),
                      ),
                    ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _IntroCard extends StatelessWidget {
  const _IntroCard({required this.revision});

  final int revision;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('离线也能听懂的口令', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('设置常用口令，断网时设备也能直接执行，不用等网络。'),
            if (revision > 0) ...[
              const SizedBox(height: 10),
              Text(
                '已发布到第 $revision 版',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _EmptyCard extends StatelessWidget {
  const _EmptyCard();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Icon(
              Icons.record_voice_over_outlined,
              size: 48,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text('还没有本地指令', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('点右下角新增一条口令，比如「听故事」。', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _CommandCard extends ConsumerWidget {
  const _CommandCard({required this.command});

  final LocalCommand command;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: ListTile(
        contentPadding: const EdgeInsets.fromLTRB(18, 8, 8, 8),
        leading: CircleAvatar(
          backgroundColor: Theme.of(context).colorScheme.primaryContainer,
          child: Icon(_actionIcon(command.action)),
        ),
        title: Text(command.phrase),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(_subtitle(command)),
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Switch(
              value: command.enabled,
              onChanged: (_) => ref
                  .read(localCommandControllerProvider.notifier)
                  .toggleEnabled(command.commandId),
            ),
            IconButton(
              tooltip: '编辑',
              onPressed: () => _openEditor(context, ref, command),
              icon: const Icon(Icons.edit_outlined),
            ),
            IconButton(
              tooltip: '删除',
              onPressed: () => _confirmDelete(context, ref, command),
              icon: const Icon(Icons.delete_outline_rounded),
            ),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry, super.key});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_off_outlined,
              size: 46,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

Future<void> _openEditor(
  BuildContext context,
  WidgetRef ref,
  LocalCommand? existing,
) async {
  final result = await showModalBottomSheet<_CommandDraft>(
    context: context,
    isScrollControlled: true,
    builder: (context) => _CommandEditor(existing: existing),
  );
  if (result == null) {
    return;
  }
  final current = ref.read(localCommandControllerProvider).value;
  if (current == null) {
    return;
  }
  final updated = <LocalCommand>[
    for (final command in current.commands)
      if (command.commandId != result.commandId) command,
    result.toCommand(revision: current.revision),
  ];
  try {
    await ref.read(localCommandControllerProvider.notifier).save(updated);
  } on Object catch (error) {
    if (context.mounted) {
      _showError(context, LocalCommandController.messageFor(error));
    }
  }
}

Future<void> _confirmDelete(
  BuildContext context,
  WidgetRef ref,
  LocalCommand command,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('删除本地指令'),
      content: Text('删除「${command.phrase}」后，设备将不再响应这条口令。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('保留'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('删除'),
        ),
      ],
    ),
  );
  if (confirmed != true) {
    return;
  }
  try {
    await ref
        .read(localCommandControllerProvider.notifier)
        .delete(command.commandId);
  } on Object catch (error) {
    if (context.mounted) {
      _showError(context, LocalCommandController.messageFor(error));
    }
  }
}

void _showError(BuildContext context, String message) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(message)));
}

class _CommandDraft {
  const _CommandDraft({
    required this.commandId,
    required this.phrase,
    required this.action,
    required this.argument,
    required this.enabled,
  });

  final String commandId;
  final String phrase;
  final LocalCommandAction action;
  final String argument;
  final bool enabled;

  LocalCommand toCommand({required int revision}) {
    return LocalCommand(
      commandId: commandId,
      phrase: phrase,
      action: action,
      argument: argument,
      enabled: enabled,
      revision: revision,
    );
  }
}

class _CommandEditor extends StatefulWidget {
  const _CommandEditor({required this.existing});

  final LocalCommand? existing;

  @override
  State<_CommandEditor> createState() => _CommandEditorState();
}

class _CommandEditorState extends State<_CommandEditor> {
  late final TextEditingController _phraseController;
  late final TextEditingController _argumentController;
  late LocalCommandAction _action;
  late bool _enabled;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    final existing = widget.existing;
    _phraseController = TextEditingController(text: existing?.phrase ?? '');
    _argumentController = TextEditingController(
      text: existing?.argument ?? '',
    );
    _action = existing?.action ?? LocalCommandAction.playPackage;
    _enabled = existing?.enabled ?? true;
  }

  @override
  void dispose() {
    _phraseController.dispose();
    _argumentController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final existing = widget.existing;
    return Padding(
      padding: EdgeInsets.only(
        left: 20,
        right: 20,
        top: 20,
        bottom: MediaQuery.of(context).viewInsets.bottom + 20,
      ),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              existing == null ? '新增本地指令' : '编辑本地指令',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _phraseController,
              decoration: const InputDecoration(
                labelText: '口令',
                hintText: '例如：听故事',
                border: OutlineInputBorder(),
              ),
              maxLength: 48,
            ),
            const SizedBox(height: 8),
            DropdownButtonFormField<LocalCommandAction>(
              initialValue: _action,
              decoration: const InputDecoration(
                labelText: '动作',
                border: OutlineInputBorder(),
              ),
              items: [
                for (final action in LocalCommandAction.values)
                  DropdownMenuItem<LocalCommandAction>(
                    value: action,
                    child: Text(action.label),
                  ),
              ],
              onChanged: (value) {
                if (value == null) {
                  return;
                }
                setState(() {
                  _action = value;
                  if (!value.requiresArgument) {
                    _argumentController.clear();
                  }
                });
              },
            ),
            if (_action.requiresArgument) ...[
              const SizedBox(height: 12),
              TextField(
                controller: _argumentController,
                decoration: InputDecoration(
                  labelText: _action == LocalCommandAction.playPackage
                      ? '内容标识'
                      : '音量（0-100）',
                  border: const OutlineInputBorder(),
                ),
                maxLength: 80,
              ),
            ],
            const SizedBox(height: 8),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('启用'),
              subtitle: const Text('关闭后设备不会响应这条口令'),
              value: _enabled,
              onChanged: (value) => setState(() => _enabled = value),
            ),
            if (_errorMessage != null) ...[
              const SizedBox(height: 8),
              Text(
                _errorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 16),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('取消'),
                ),
                const SizedBox(width: 8),
                FilledButton(
                  onPressed: _submit,
                  child: const Text('保存'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  void _submit() {
    final phrase = _phraseController.text.trim();
    if (phrase.isEmpty) {
      setState(() => _errorMessage = '口令不能为空');
      return;
    }
    final argument = _action.requiresArgument
        ? _argumentController.text.trim()
        : '';
    if (_action.requiresArgument && argument.isEmpty) {
      setState(() {
        _errorMessage = _action == LocalCommandAction.playPackage
            ? '请填写要播放的内容标识'
            : '请填写音量';
      });
      return;
    }
    final commandId = widget.existing?.commandId ?? _generateCommandId(phrase);
    Navigator.of(context).pop(
      _CommandDraft(
        commandId: commandId,
        phrase: phrase,
        action: _action,
        argument: argument,
        enabled: _enabled,
      ),
    );
  }
}

String _generateCommandId(String phrase) {
  final digest = phrase.codeUnits.fold<int>(
    17,
    (value, unit) => (value * 31 + unit) & 0x7fffffff,
  );
  return 'cmd_${digest.toRadixString(36)}';
}

String _subtitle(LocalCommand command) {
  final parts = <String>[
    command.action.label,
    if (command.argument.isNotEmpty) command.argument,
    if (!command.enabled) '已停用',
  ];
  return parts.join(' · ');
}

IconData _actionIcon(LocalCommandAction action) {
  return switch (action) {
    LocalCommandAction.playPackage => Icons.play_circle_outline_rounded,
    LocalCommandAction.pause => Icons.pause_circle_outline_rounded,
    LocalCommandAction.resume => Icons.play_arrow_rounded,
    LocalCommandAction.stop => Icons.stop_circle_outlined,
    LocalCommandAction.setVolume => Icons.volume_down_rounded,
    LocalCommandAction.sleep => Icons.bedtime_outlined,
  };
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/me');
  }
}
