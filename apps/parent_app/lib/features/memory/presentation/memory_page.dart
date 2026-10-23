import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/memory_controller.dart';
import '../domain/child_memory.dart';

/// Guardian view of what the robot remembers about one child.
///
/// Every value here can be a child's personal information, so the screen is
/// reachable only from the guardian's own settings and offers explicit deletion
/// for each entry as well as for the whole store.
class MemoryPage extends ConsumerStatefulWidget {
  const MemoryPage({super.key});

  @override
  ConsumerState<MemoryPage> createState() => _MemoryPageState();
}

class _MemoryPageState extends ConsumerState<MemoryPage> {
  String? _childId;

  @override
  Widget build(BuildContext context) {
    final children = ref.watch(childProfileControllerProvider);
    final state = ref.watch(childMemoryControllerProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text('记忆'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: children.when(
        loading: () => const _LoadingState(),
        error: (error, _) => _ErrorState(
          message: _errorMessage(error),
          onRetry: () => ref.invalidate(childProfileControllerProvider),
        ),
        data: (profiles) {
          if (profiles.isEmpty) {
            return const _EmptyState(
              title: '还没有儿童档案',
              description: '添加儿童档案后，机器人和孩子的对话记忆会显示在这里。',
            );
          }
          final selected = _resolveChildId(profiles);
          return Column(
            children: <Widget>[
              if (profiles.length > 1)
                _ChildSelector(
                  children: profiles,
                  selectedId: selected,
                  counts:
                      ref.watch(familyMemoryCountsProvider).value ??
                      const <String, int>{},
                  onSelected: (childId) {
                    setState(() => _childId = childId);
                    ref
                        .read(childMemoryControllerProvider.notifier)
                        .load(childId);
                  },
                ),
              Expanded(
                child: _MemoryBody(
                  state: state,
                  childId: selected,
                  onReload: () => ref
                      .read(childMemoryControllerProvider.notifier)
                      .load(selected),
                  onAdd: () => _showEditSheet(context, selected, null),
                  onEdit: (memory) =>
                      _showEditSheet(context, selected, memory),
                  onDelete: (memory) => _confirmDelete(memory, selected),
                  onClear: () => _confirmClear(selected),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  /// Picks the remembered child, or the first one, and loads its memories on
  /// first build so the screen is never empty without a reason.
  String _resolveChildId(List<ChildProfile> profiles) {
    final selected = _childId;
    if (selected != null && profiles.any((child) => child.childId == selected)) {
      return selected;
    }
    final first = profiles.first.childId;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _childId == first) {
        return;
      }
      setState(() => _childId = first);
      ref.read(childMemoryControllerProvider.notifier).load(first);
    });
    return first;
  }

  Future<void> _showEditSheet(
    BuildContext context,
    String childId,
    ChildMemory? existing,
  ) async {
    final keyController = TextEditingController(text: existing?.memoryKey);
    final valueController = TextEditingController(text: existing?.memoryValue);
    final saved = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) {
        return Padding(
          padding: EdgeInsets.only(
            left: 20,
            right: 20,
            top: 20,
            bottom: MediaQuery.of(sheetContext).viewInsets.bottom + 20,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(
                existing == null ? '添加一条记忆' : '修改这条记忆',
                style: Theme.of(sheetContext).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),
              TextField(
                controller: keyController,
                enabled: existing == null,
                decoration: const InputDecoration(
                  labelText: '记忆标识',
                  helperText: '例如 favorite_color、pet_name',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: valueController,
                maxLength: 280,
                decoration: const InputDecoration(labelText: '记忆内容'),
              ),
              const SizedBox(height: 16),
              FilledButton(
                onPressed: () => Navigator.of(sheetContext).pop(true),
                child: const Text('保存记忆'),
              ),
            ],
          ),
        );
      },
    );
    if (saved != true) {
      keyController.dispose();
      valueController.dispose();
      return;
    }
    await ref
        .read(childMemoryControllerProvider.notifier)
        .save(
          childId: childId,
          memoryKey: keyController.text.trim(),
          memoryValue: valueController.text.trim(),
        );
    keyController.dispose();
    valueController.dispose();
    if (mounted) {
      _showMessage('记忆已保存。之后设备再上报同一个标识时，会保留你写的内容。');
    }
  }

  Future<void> _confirmDelete(ChildMemory memory, String childId) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除这条记忆'),
        content: Text('删除后，机器人不会再提起“${memory.memoryValue}”。'),
        actions: <Widget>[
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('保留'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('删除记忆'),
          ),
        ],
      ),
    );
    if (confirmed != true) {
      return;
    }
    await ref
        .read(childMemoryControllerProvider.notifier)
        .remove(childId: childId, memoryKey: memory.memoryKey);
    if (mounted) {
      _showMessage('记忆已删除。');
    }
  }

  Future<void> _confirmClear(String childId) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('清空全部记忆'),
        content: const Text('删除后无法恢复，机器人将忘记关于这个孩子的所有记忆。'),
        actions: <Widget>[
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('保留'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('清空记忆'),
          ),
        ],
      ),
    );
    if (confirmed != true) {
      return;
    }
    await ref
        .read(childMemoryControllerProvider.notifier)
        .clear(childId);
    if (mounted) {
      _showMessage('记忆已清空。');
    }
  }

  void _showMessage(String message) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

class _MemoryBody extends StatelessWidget {
  const _MemoryBody({
    required this.state,
    required this.childId,
    required this.onReload,
    required this.onAdd,
    required this.onEdit,
    required this.onDelete,
    required this.onClear,
  });

  final AsyncValue<ChildMemoryState> state;
  final String childId;
  final VoidCallback onReload;
  final VoidCallback onAdd;
  final ValueChanged<ChildMemory> onEdit;
  final ValueChanged<ChildMemory> onDelete;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    return state.when(
      loading: () => const _LoadingState(),
      error: (error, _) => _ErrorState(
        message: _errorMessage(error),
        onRetry: onReload,
      ),
      data: (value) {
        if (value.isEmpty) {
          return _EmptyState(
            title: '还没有记忆',
            description: '孩子和机器人聊天后，机器人会记下一些小事。你也可以在这里直接添加。',
            action: FilledButton(onPressed: onAdd, child: const Text('添加一条记忆')),
          );
        }
        return ListView.separated(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 96),
          itemCount: value.memories.length + 1,
          separatorBuilder: (_, _) => const SizedBox(height: 10),
          itemBuilder: (context, index) {
            if (index == value.memories.length) {
              return Padding(
                padding: const EdgeInsets.only(top: 12),
                child: OutlinedButton(
                  onPressed: onClear,
                  child: const Text('清空全部记忆'),
                ),
              );
            }
            final memory = value.memories[index];
            return _MemoryTile(
              memory: memory,
              onEdit: () => onEdit(memory),
              onDelete: () => onDelete(memory),
            );
          },
        );
      },
    );
  }
}

class _MemoryTile extends StatelessWidget {
  const _MemoryTile({
    required this.memory,
    required this.onEdit,
    required this.onDelete,
  });

  final ChildMemory memory;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 8, 12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    memory.memoryKey,
                    style: Theme.of(context).textTheme.labelMedium,
                  ),
                  const SizedBox(height: 4),
                  Text(
                    memory.memoryValue,
                    style: Theme.of(context).textTheme.bodyLarge,
                  ),
                  const SizedBox(height: 6),
                  Text(
                    memory.source.label,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            IconButton(
              tooltip: '修改记忆',
              onPressed: onEdit,
              icon: const Icon(Icons.edit_outlined),
            ),
            IconButton(
              tooltip: '删除记忆',
              onPressed: onDelete,
              icon: const Icon(Icons.delete_outline_rounded),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChildSelector extends StatelessWidget {
  const _ChildSelector({
    required this.children,
    required this.selectedId,
    required this.onSelected,
    required this.counts,
  });

  final List<ChildProfile> children;
  final String selectedId;
  final ValueChanged<String> onSelected;

  /// Memories per child across the whole family. A child absent from the map has
  /// none, which is why the label stays plain instead of showing a zero.
  final Map<String, int> counts;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
      child: Row(
        children: <Widget>[
          for (final child in children)
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: ChoiceChip(
                label: Text(_labelFor(child)),
                selected: child.childId == selectedId,
                onSelected: (_) => onSelected(child.childId),
              ),
            ),
        ],
      ),
    );
  }

  String _labelFor(ChildProfile child) {
    final count = counts[child.childId] ?? 0;
    if (count == 0) {
      return child.nickname;
    }
    return '${child.nickname} · $count 条';
  }
}

class _LoadingState extends StatelessWidget {
  const _LoadingState();

  @override
  Widget build(BuildContext context) {
    return const Center(child: CircularProgressIndicator());
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({
    required this.title,
    required this.description,
    this.action,
  });

  final String title;
  final String description;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(
              description,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (action != null) ...<Widget>[
              const SizedBox(height: 16),
              action!,
            ],
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

/// Maps a transport or domain failure to text a guardian can act on.
///
/// Raw exception text never reaches the screen: it can carry a request path or
/// an internal status that means nothing to a parent.
String _errorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时读不到记忆，请稍后重新加载。';
}
