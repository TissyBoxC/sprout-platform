import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../auth/application/auth_controller.dart';
import '../../auth/data/auth_api.dart';

/// Guardian profile, AI account summary, and account actions.
class ProfilePage extends ConsumerWidget {
  const ProfilePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authControllerProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('我的')),
      body: AppStateSwitcher(
        stateKey: auth.when(
          data: (value) => value.account == null ? 'signed-out' : 'content',
          error: (_, _) => 'error',
          loading: () => 'loading',
        ),
        child: auth.when(
          loading: () => const Center(
            key: ValueKey<String>('loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ProfileErrorState(
            key: const ValueKey<String>('error'),
            message: authErrorMessage(error),
            onRetry: () =>
                ref.read(authControllerProvider.notifier).refreshAccount(),
          ),
          data: (state) {
            final account = state.account;
            if (account == null) {
              return const Center(
                key: ValueKey<String>('signed-out'),
                child: Text('请先登录家长账号'),
              );
            }
            return ListView(
              key: const ValueKey<String>('content'),
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
              children: [
                AppReveal(child: _ProfileHeader(account: account)),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 60),
                  child: _ProfileInfoCard(
                    account: account,
                    onEdit: () => _openProfileEditor(context, account),
                  ),
                ),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 110),
                  child: _AiAccountCard(
                    aiAccount: state.aiAccount,
                    onRetry: () => ref
                        .read(authControllerProvider.notifier)
                        .retryAIService(),
                    onModelsChanged: (models) => ref
                        .read(authControllerProvider.notifier)
                        .updateSelectedModels(models),
                  ),
                ),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 160),
                  child: _SettingsCard(
                    account: account,
                    onChildren: () => context.push('/me/children'),
                    onEmail: () => context.push('/account/email'),
                    onUpdate: () => context.push('/me/update'),
                    onLogout: () => _confirmLogout(context, ref),
                  ),
                ),
              ],
            );
          },
        ),
      ),
    );
  }
}

class _ProfileHeader extends StatelessWidget {
  const _ProfileHeader({required this.account});

  final ParentAccount account;

  @override
  Widget build(BuildContext context) {
    final name = account.displayName.isNotEmpty ? account.displayName : '家长';
    final childName = account.childNickname.isNotEmpty
        ? account.childNickname
        : '宝贝';
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          children: [
            CircleAvatar(
              radius: 30,
              backgroundColor: Theme.of(context).colorScheme.primaryContainer,
              child: Text(
                name.characters.first,
                style: Theme.of(context).textTheme.titleLarge?.copyWith(
                  color: Theme.of(context).colorScheme.primary,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(name, style: Theme.of(context).textTheme.titleLarge),
                  const SizedBox(height: 4),
                  Text('陪伴 $childName 一起成长'),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ProfileInfoCard extends StatelessWidget {
  const _ProfileInfoCard({required this.account, required this.onEdit});

  final ParentAccount account;
  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 12, 18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Text('家长资料', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                TextButton.icon(
                  onPressed: onEdit,
                  icon: const Icon(Icons.edit_outlined, size: 18),
                  label: const Text('修改资料'),
                ),
              ],
            ),
            const SizedBox(height: 8),
            _InfoRow(label: '家长称呼', value: account.displayName),
            _InfoRow(
              label: '家长姓氏',
              value: account.guardianFamilyName.isEmpty
                  ? '未填写'
                  : account.guardianFamilyName,
            ),
            _InfoRow(
              label: '宝贝姓名',
              value: account.childNickname.isEmpty
                  ? '未填写'
                  : account.childNickname,
            ),
            _InfoRow(
              label: '宝贝生日',
              value: account.childBirthday.isEmpty
                  ? '未填写'
                  : account.childBirthday,
            ),
            _InfoRow(label: '手机号', value: account.phone),
          ],
        ),
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  const _InfoRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          SizedBox(
            width: 82,
            child: Text(label, style: Theme.of(context).textTheme.bodySmall),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              value.isEmpty ? '未填写' : value,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: Theme.of(context).colorScheme.onSurface,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _AiAccountCard extends StatelessWidget {
  const _AiAccountCard({
    required this.aiAccount,
    required this.onRetry,
    required this.onModelsChanged,
  });

  final AiAccount? aiAccount;
  final Future<void> Function() onRetry;
  final Future<void> Function(List<String>) onModelsChanged;

  @override
  Widget build(BuildContext context) {
    final account = aiAccount;
    if (account == null) {
      return Card(
        child: ListTile(
          leading: const Icon(Icons.cloud_off_outlined),
          title: const Text('AI 陪伴服务正在准备'),
          subtitle: const Text('准备期间不会产生费用，也不用重新注册。'),
          trailing: TextButton(onPressed: onRetry, child: const Text('重新准备')),
        ),
      );
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.auto_awesome_outlined),
                const SizedBox(width: 10),
                Text('AI 陪伴账号', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: _SummaryMetric(
                    label: '可用额度',
                    value: account.balanceUsd.toStringAsFixed(2),
                  ),
                ),
                Container(
                  width: 1,
                  height: 42,
                  color: Theme.of(context).colorScheme.outlineVariant,
                ),
                Expanded(
                  child: _SummaryMetric(
                    label: '同时对话',
                    value: '${account.concurrencyLimit} 台',
                  ),
                ),
              ],
            ),
            const SizedBox(height: 18),
            Text('对话模型', style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: 6),
            Text(
              account.selectedModels.isEmpty
                  ? '使用全部已开通模型'
                  : account.selectedModels.join('、'),
            ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerLeft,
              child: OutlinedButton.icon(
                onPressed: () => _showModelPicker(context, account),
                icon: const Icon(Icons.tune_rounded),
                label: const Text('选择可用模型'),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _showModelPicker(BuildContext context, AiAccount account) async {
    final selectedModels = <String>{
      if (account.selectedModels.isEmpty)
        ...account.availableModels
      else
        ...account.selectedModels,
    };
    final saved = await showModalBottomSheet<bool>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) => StatefulBuilder(
        builder: (context, setModalState) => SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text('选择对话模型', style: Theme.of(context).textTheme.titleLarge),
                const SizedBox(height: 6),
                const Text('只勾选允许孩子使用的模型，至少要保留一个。'),
                const SizedBox(height: 12),
                if (account.availableModels.isEmpty)
                  const ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(Icons.info_outline_rounded),
                    title: Text('当前没有可选模型'),
                    subtitle: Text('请稍后重新准备 AI 服务。'),
                  )
                else
                  Flexible(
                    child: SingleChildScrollView(
                      child: Column(
                        children: [
                          for (final model in account.availableModels)
                            CheckboxListTile(
                              value: selectedModels.contains(model),
                              contentPadding: EdgeInsets.zero,
                              title: Text(model),
                              onChanged: (isSelected) => setModalState(() {
                                if (isSelected == true) {
                                  selectedModels.add(model);
                                } else {
                                  selectedModels.remove(model);
                                }
                              }),
                            ),
                        ],
                      ),
                    ),
                  ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: selectedModels.isEmpty
                      ? null
                      : () => Navigator.of(context).pop(true),
                  child: const Text('保存模型选择'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
    if (saved == true) {
      await onModelsChanged(selectedModels.toList(growable: false));
    }
  }
}

class _SummaryMetric extends StatelessWidget {
  const _SummaryMetric({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: Theme.of(context).textTheme.bodySmall),
        const SizedBox(height: 4),
        FittedBox(
          fit: BoxFit.scaleDown,
          alignment: Alignment.centerLeft,
          child: Text(
            value,
            style: Theme.of(
              context,
            ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
          ),
        ),
      ],
    );
  }
}

class _SettingsCard extends StatelessWidget {
  const _SettingsCard({
    required this.account,
    required this.onChildren,
    required this.onEmail,
    required this.onUpdate,
    required this.onLogout,
  });

  final ParentAccount account;
  final VoidCallback onChildren;
  final VoidCallback onEmail;
  final VoidCallback onUpdate;
  final VoidCallback onLogout;

  @override
  Widget build(BuildContext context) {
    final hasEmail = account.email.isNotEmpty;
    return Card(
      clipBehavior: Clip.antiAlias,
      child: Column(
        children: [
          ListTile(
            leading: const Icon(Icons.child_care_rounded),
            title: const Text('儿童档案'),
            subtitle: const Text('管理宝贝资料、兴趣和使用边界'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: onChildren,
          ),
          const Divider(height: 1, indent: 16, endIndent: 16),
          ListTile(
            leading: const Icon(Icons.mark_email_read_outlined),
            title: Text(hasEmail ? account.email : '绑定登录邮箱'),
            subtitle: Text(hasEmail ? '可以使用手机号或邮箱登录' : '绑定后可以用邮箱登录，也方便找回账号'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: onEmail,
          ),
          const Divider(height: 1, indent: 16, endIndent: 16),
          ListTile(
            leading: const Icon(Icons.system_update_alt_rounded),
            title: const Text('软件更新'),
            subtitle: const Text('检查新功能和内容更新'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: onUpdate,
          ),
          const Divider(height: 1, indent: 16, endIndent: 16),
          ListTile(
            leading: Icon(
              Icons.logout_rounded,
              color: Theme.of(context).colorScheme.error,
            ),
            title: Text(
              '退出登录',
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
            onTap: onLogout,
          ),
        ],
      ),
    );
  }
}

class _ProfileErrorState extends StatelessWidget {
  const _ProfileErrorState({
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
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

Future<void> _openProfileEditor(BuildContext context, ParentAccount account) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) => _ProfileEditPage(account: account),
    ),
  );
}

class _ProfileEditPage extends ConsumerStatefulWidget {
  const _ProfileEditPage({required this.account});

  final ParentAccount account;

  @override
  ConsumerState<_ProfileEditPage> createState() => _ProfileEditPageState();
}

class _ProfileEditPageState extends ConsumerState<_ProfileEditPage> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _displayNameController;
  late final TextEditingController _guardianFamilyNameController;
  late final TextEditingController _childNicknameController;
  late final TextEditingController _childBirthdayController;
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    final account = widget.account;
    _displayNameController = TextEditingController(text: account.displayName);
    _guardianFamilyNameController = TextEditingController(
      text: account.guardianFamilyName,
    );
    _childNicknameController = TextEditingController(
      text: account.childNickname,
    );
    _childBirthdayController = TextEditingController(
      text: account.childBirthday,
    );
  }

  @override
  void dispose() {
    _displayNameController.dispose();
    _guardianFamilyNameController.dispose();
    _childNicknameController.dispose();
    _childBirthdayController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      await ref
          .read(authControllerProvider.notifier)
          .updateProfile(
            displayName: _displayNameController.text.trim(),
            guardianFamilyName: _guardianFamilyNameController.text.trim(),
            childNickname: _childNicknameController.text.trim(),
            childBirthday: _childBirthdayController.text.trim(),
          );
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = authErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('修改资料')),
      body: SafeArea(
        child: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              TextFormField(
                controller: _displayNameController,
                decoration: const InputDecoration(labelText: '家长称呼'),
                validator: (value) {
                  if (value == null || value.trim().isEmpty) {
                    return '请输入家长称呼';
                  }
                  if (value.trim().length > 40) {
                    return '家长称呼不能超过 40 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _guardianFamilyNameController,
                decoration: const InputDecoration(labelText: '家长姓氏（可选）'),
                validator: (value) {
                  if ((value?.trim().length ?? 0) > 40) {
                    return '家长姓氏不能超过 40 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _childNicknameController,
                decoration: const InputDecoration(labelText: '宝贝姓名（可选）'),
                validator: (value) {
                  if ((value?.trim().length ?? 0) > 40) {
                    return '宝贝姓名不能超过 40 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _childBirthdayController,
                keyboardType: TextInputType.datetime,
                decoration: const InputDecoration(
                  labelText: '宝贝生日（可选）',
                  hintText: '例如 2021-06-01',
                ),
                validator: (value) {
                  final birthday = value?.trim() ?? '';
                  if (birthday.isEmpty) {
                    return null;
                  }
                  final parsed = DateTime.tryParse(birthday);
                  if (parsed == null || parsed.isAfter(DateTime.now())) {
                    return '请输入有效日期，例如 2021-06-01';
                  }
                  return null;
                },
              ),
              if (_errorMessage != null) ...[
                const SizedBox(height: 14),
                Text(
                  _errorMessage!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ],
              const SizedBox(height: 24),
              FilledButton(
                onPressed: _isSubmitting ? null : _submit,
                child: Text(_isSubmitting ? '正在保存…' : '保存资料'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

Future<void> _confirmLogout(BuildContext context, WidgetRef ref) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('退出登录'),
      content: const Text('退出后需要重新输入手机号或邮箱登录，设备绑定不会解除。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('继续使用'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('退出登录'),
        ),
      ],
    ),
  );
  if (confirmed == true) {
    await ref.read(authControllerProvider.notifier).logout();
  }
}
