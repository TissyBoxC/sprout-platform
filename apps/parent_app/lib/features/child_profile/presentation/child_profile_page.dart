import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/child_profile_controller.dart';
import '../domain/child_profile.dart';

/// Lists child profiles and exposes create, edit, policy, and delete actions.
class ChildProfilePage extends ConsumerWidget {
  const ChildProfilePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profiles = ref.watch(childProfileControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('儿童档案'),
        actions: [
          IconButton(
            tooltip: '添加宝贝档案',
            onPressed: () => _openEditor(context, ref),
            icon: const Icon(Icons.add_circle_outline_rounded),
          ),
          const SizedBox(width: 4),
        ],
      ),
      body: AppStateSwitcher(
        stateKey: profiles.when(
          data: (items) => items.isEmpty ? 'child-empty' : 'child-content',
          error: (_, _) => 'child-error',
          loading: () => 'child-loading',
        ),
        child: profiles.when(
          loading: () => const Center(
            key: ValueKey<String>('child-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ChildProfileErrorState(
            key: const ValueKey<String>('child-error'),
            message: childProfileErrorMessage(error),
            onRetry: () =>
                ref.read(childProfileControllerProvider.notifier).refresh(),
          ),
          data: (items) {
            if (items.isEmpty) {
              return _ChildProfileEmptyState(
                key: const ValueKey<String>('child-empty'),
                onCreate: () => _openEditor(context, ref),
              );
            }
            return RefreshIndicator(
              key: const ValueKey<String>('child-content'),
              onRefresh: () =>
                  ref.read(childProfileControllerProvider.notifier).refresh(),
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
                children: [
                  AppReveal(
                    child: Card(
                      child: Padding(
                        padding: const EdgeInsets.all(18),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              '让陪伴更适合每个孩子',
                              style: Theme.of(context).textTheme.titleMedium,
                            ),
                            const SizedBox(height: 6),
                            const Text('年龄、兴趣和内容分类会影响推荐，也决定设备上的安全边界。'),
                            const SizedBox(height: 14),
                            FilledButton.icon(
                              onPressed: () => _openEditor(context, ref),
                              icon: const Icon(Icons.add_rounded),
                              label: const Text('添加宝贝档案'),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 18),
                  for (var index = 0; index < items.length; index++)
                    AppReveal(
                      delay: Duration(milliseconds: 50 * index.clamp(0, 5)),
                      child: Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: _ChildProfileCard(
                          profile: items[index],
                          onEdit: () =>
                              _openEditor(context, ref, profile: items[index]),
                          onPolicy: () => context.push(
                            '/me/children/${items[index].childId}/policy',
                          ),
                          onDelete: () =>
                              _confirmDelete(context, ref, items[index]),
                        ),
                      ),
                    ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}

class _ChildProfileCard extends StatelessWidget {
  const _ChildProfileCard({
    required this.profile,
    required this.onEdit,
    required this.onPolicy,
    required this.onDelete,
  });

  final ChildProfile profile;
  final VoidCallback onEdit;
  final VoidCallback onPolicy;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 16, 8, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                CircleAvatar(
                  radius: 24,
                  backgroundColor: Theme.of(
                    context,
                  ).colorScheme.primaryContainer,
                  child: Text(
                    profile.nickname.characters.first,
                    style: Theme.of(context).textTheme.titleMedium?.copyWith(
                      color: Theme.of(context).colorScheme.primary,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        profile.nickname,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 3),
                      Text(profile.ageTier.label),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 14),
            _TagSection(
              label: '兴趣',
              values: profile.interests.isEmpty
                  ? const ['暂未选择']
                  : profile.interests
                        .map((interest) => _interestLabel(interest))
                        .toList(growable: false),
            ),
            const SizedBox(height: 10),
            _TagSection(
              label: '内容',
              values: profile.contentCategories
                  .map((category) => category.label)
                  .toList(growable: false),
            ),
            const SizedBox(height: 6),
            Row(
              children: [
                TextButton.icon(
                  onPressed: onEdit,
                  icon: const Icon(Icons.edit_outlined, size: 18),
                  label: const Text('修改档案'),
                ),
                TextButton.icon(
                  onPressed: onPolicy,
                  icon: const Icon(Icons.tune_rounded, size: 18),
                  label: const Text('时间与内容'),
                ),
                const Spacer(),
                IconButton(
                  tooltip: '删除宝贝档案',
                  onPressed: onDelete,
                  icon: const Icon(Icons.delete_outline_rounded),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _TagSection extends StatelessWidget {
  const _TagSection({required this.label, required this.values});

  final String label;
  final List<String> values;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 42,
          child: Padding(
            padding: const EdgeInsets.only(top: 5),
            child: Text(label, style: Theme.of(context).textTheme.bodySmall),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (final value in values)
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 5,
                  ),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.surfaceContainerLowest,
                    borderRadius: BorderRadius.circular(99),
                    border: Border.all(
                      color: Theme.of(context).colorScheme.outlineVariant,
                    ),
                  ),
                  child: Text(
                    value,
                    style: Theme.of(context).textTheme.labelMedium,
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _ChildProfileEmptyState extends StatelessWidget {
  const _ChildProfileEmptyState({required this.onCreate, super.key});

  final VoidCallback onCreate;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 88,
              height: 88,
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.primaryContainer,
                shape: BoxShape.circle,
              ),
              child: Icon(
                Icons.child_care_rounded,
                size: 44,
                color: Theme.of(context).colorScheme.primary,
              ),
            ),
            const SizedBox(height: 18),
            Text('还没有宝贝档案', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 8),
            const Text(
              '添加后可以设置年龄、兴趣和使用边界，设备会按这份档案提供内容。',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 20),
            FilledButton.icon(
              onPressed: onCreate,
              icon: const Icon(Icons.add_rounded),
              label: const Text('添加宝贝档案'),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChildProfileErrorState extends StatelessWidget {
  const _ChildProfileErrorState({
    required this.message,
    required this.onRetry,
    super.key,
  });

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.cloud_off_outlined, size: 42),
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
  WidgetRef ref, {
  ChildProfile? profile,
}) async {
  final wasSaved = await Navigator.of(context).push<bool>(
    MaterialPageRoute<bool>(
      builder: (context) => ChildProfileEditPage(profile: profile),
    ),
  );
  if (wasSaved == true && context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(profile == null ? '宝贝档案已添加' : '宝贝档案已更新')),
    );
  }
}

/// Creates or edits one child profile.
class ChildProfileEditPage extends ConsumerStatefulWidget {
  const ChildProfileEditPage({this.profile, super.key});

  final ChildProfile? profile;

  @override
  ConsumerState<ChildProfileEditPage> createState() =>
      _ChildProfileEditPageState();
}

class _ChildProfileEditPageState extends ConsumerState<ChildProfileEditPage> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _nicknameController;
  late ChildAgeTier _ageTier;
  late Set<String> _interests;
  late Set<ChildContentCategory> _categories;
  late bool _guardianConsent;
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    final profile = widget.profile;
    _nicknameController = TextEditingController(text: profile?.nickname ?? '');
    _ageTier = profile?.ageTier ?? ChildAgeTier.age5To6;
    _interests = {...?profile?.interests};
    _categories =
        profile?.contentCategories.toSet() ?? {...ChildContentCategory.values};
    _guardianConsent = profile?.guardianConsent ?? false;
  }

  @override
  void dispose() {
    _nicknameController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    if (!_guardianConsent) {
      setState(() => _errorMessage = '请确认已获得监护人同意');
      return;
    }
    if (_categories.isEmpty) {
      setState(() => _errorMessage = '请至少选择一个内容分类');
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    final draft = ChildProfileDraft(
      nickname: _nicknameController.text.trim(),
      ageTier: _ageTier,
      interests: _interests.toList(growable: false),
      contentCategories: _categories.toList(growable: false),
      guardianConsent: _guardianConsent,
    );
    try {
      final controller = ref.read(childProfileControllerProvider.notifier);
      final profile = widget.profile;
      if (profile == null) {
        await controller.create(draft);
      } else {
        await controller.saveProfile(profile.childId, draft);
      }
      if (mounted) {
        Navigator.of(context).pop(true);
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = childProfileErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final isEditing = widget.profile != null;
    return Scaffold(
      appBar: AppBar(title: Text(isEditing ? '修改宝贝档案' : '添加宝贝档案')),
      body: SafeArea(
        child: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 32),
            children: [
              TextFormField(
                controller: _nicknameController,
                textInputAction: TextInputAction.next,
                maxLength: 32,
                decoration: const InputDecoration(
                  labelText: '宝贝称呼',
                  hintText: '例如：小芽',
                  counterText: '',
                ),
                validator: (value) {
                  final nickname = value?.trim() ?? '';
                  if (nickname.isEmpty) {
                    return '请输入宝贝称呼';
                  }
                  if (nickname.length > 32) {
                    return '宝贝称呼不能超过 32 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 18),
              Text('年龄段', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 10),
              SegmentedButton<ChildAgeTier>(
                segments: [
                  for (final tier in ChildAgeTier.values)
                    ButtonSegment<ChildAgeTier>(
                      value: tier,
                      label: Text(tier.label),
                    ),
                ],
                selected: {_ageTier},
                onSelectionChanged: (selection) {
                  setState(() => _ageTier = selection.first);
                },
              ),
              const SizedBox(height: 22),
              Text('兴趣', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 4),
              const Text('可以多选，也可以跳过。'),
              const SizedBox(height: 10),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final interest in childInterestSuggestions)
                    FilterChip(
                      label: Text(childInterestLabels[interest] ?? interest),
                      selected: _interests.contains(interest),
                      onSelected: (selected) {
                        setState(() {
                          if (selected) {
                            _interests.add(interest);
                          } else {
                            _interests.remove(interest);
                          }
                        });
                      },
                    ),
                ],
              ),
              const SizedBox(height: 22),
              Text('内容分类', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 4),
              const Text('至少选择一项，设备只会播放已允许的内容。'),
              const SizedBox(height: 10),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final category in ChildContentCategory.values)
                    FilterChip(
                      label: Text(category.label),
                      selected: _categories.contains(category),
                      onSelected: (selected) {
                        setState(() {
                          if (selected) {
                            _categories.add(category);
                          } else {
                            _categories.remove(category);
                          }
                        });
                      },
                    ),
                ],
              ),
              const SizedBox(height: 20),
              CheckboxListTile(
                value: _guardianConsent,
                onChanged: (value) {
                  setState(() => _guardianConsent = value ?? false);
                },
                contentPadding: EdgeInsets.zero,
                title: const Text('我是孩子的监护人，并已获得监护人同意'),
                subtitle: const Text('用于建立安全的陪伴边界，孩子信息不会用于广告或画像。'),
                controlAffinity: ListTileControlAffinity.leading,
              ),
              if (_errorMessage != null) ...[
                const SizedBox(height: 12),
                Text(
                  _errorMessage!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ],
              const SizedBox(height: 24),
              FilledButton(
                onPressed: _isSubmitting ? null : _submit,
                child: Text(_isSubmitting ? '正在保存…' : '保存档案'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

Future<void> _confirmDelete(
  BuildContext context,
  WidgetRef ref,
  ChildProfile profile,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('删除宝贝档案'),
      content: Text('删除后无法恢复，相关的时间与内容设置也会一并移除。确定删除“${profile.nickname}”吗？'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('保留档案'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('删除档案'),
        ),
      ],
    ),
  );
  if (confirmed != true || !context.mounted) {
    return;
  }
  try {
    await ref
        .read(childProfileControllerProvider.notifier)
        .delete(profile.childId);
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('宝贝档案已删除')));
    }
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(childProfileErrorMessage(error))));
    }
  }
}

String _interestLabel(String interest) {
  return childInterestLabels[interest] ?? interest;
}
