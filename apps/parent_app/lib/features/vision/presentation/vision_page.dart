import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/vision_controller.dart';
import '../domain/vision_task.dart';

/// Guardian view of one device's camera and vision use.
///
/// The page only ever shows bounded, non-technical state: whether the device
/// has a camera, whether image upload is authorized, recent task status, and
/// short-lived recognition summaries. Captured images are never shown here.
class VisionPage extends ConsumerWidget {
  const VisionPage({required this.deviceId, super.key});

  final String deviceId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final overview = ref.watch(visionControllerProvider(deviceId));
    return Scaffold(
      appBar: AppBar(
        title: const Text('摄像头与视觉'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: overview.when(
          data: (_) => 'vision-data',
          error: (_, _) => 'vision-error',
          loading: () => 'vision-loading',
        ),
        child: overview.when(
          loading: () => const Center(
            key: ValueKey<String>('vision-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('vision-error'),
            message: VisionController.messageFor(error),
            onRetry: () =>
                ref.read(visionControllerProvider(deviceId).notifier).refresh(),
          ),
          data: (value) => RefreshIndicator(
            key: const ValueKey<String>('vision-data'),
            onRefresh: () => ref
                .read(visionControllerProvider(deviceId).notifier)
                .refresh(),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
              children: [
                AppReveal(
                  child: _CapabilityCard(
                    capability: value.capability,
                    onManageConsent: () => context.push('/me/privacy'),
                  ),
                ),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 60),
                  child: _TaskActionsCard(
                    enabled: value.capability.supported &&
                        value.capability.consentGranted,
                    onCreate: (taskType, prompt) => _createTask(
                      context,
                      ref,
                      taskType: taskType,
                      prompt: prompt,
                    ),
                  ),
                ),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 120),
                  child: _TasksCard(
                    tasks: value.tasks,
                    onCancel: (taskId) => _cancelTask(
                      context,
                      ref,
                      taskId: taskId,
                    ),
                  ),
                ),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 180),
                  child: _ResultsCard(results: value.results),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _createTask(
    BuildContext context,
    WidgetRef ref, {
    required String taskType,
    required String prompt,
  }) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(visionControllerProvider(deviceId).notifier)
          .createTask(taskType: taskType, prompt: prompt);
      messenger.showSnackBar(
        const SnackBar(content: Text('已发送到设备，稍后查看结果')),
      );
    } on Object catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(VisionController.messageFor(error))),
      );
    }
  }

  Future<void> _cancelTask(
    BuildContext context,
    WidgetRef ref, {
    required String taskId,
  }) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(visionControllerProvider(deviceId).notifier)
          .cancelTask(taskId);
    } on Object catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(VisionController.messageFor(error))),
      );
    }
  }
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
    return;
  }
  context.go('/home');
}

class _CapabilityCard extends StatelessWidget {
  const _CapabilityCard({
    required this.capability,
    required this.onManageConsent,
  });

  final VisionCapability capability;
  final VoidCallback onManageConsent;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final IconData icon;
    final String title;
    final String detail;
    if (!capability.supported) {
      icon = Icons.no_photography_outlined;
      title = '此设备没有摄像头';
      detail = '当前设备未配置摄像头，无需设置。';
    } else if (!capability.consentGranted) {
      icon = Icons.privacy_tip_outlined;
      title = '尚未授权使用照片';
      detail = '授权后，设备才会在需要时拍照识别。';
    } else {
      icon = Icons.photo_camera_outlined;
      title = '拍照识别已授权';
      detail = '识别结果最多保留 ${capability.retentionHours} 小时。';
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, size: 32, color: theme.colorScheme.primary),
            const SizedBox(width: 16),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: theme.textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 6),
                  Text(detail, style: theme.textTheme.bodyMedium),
                ],
              ),
            ),
            if (capability.supported && !capability.consentGranted)
              TextButton(
                onPressed: onManageConsent,
                child: const Text('去授权'),
              ),
          ],
        ),
      ),
    );
  }
}

class _TaskActionsCard extends StatelessWidget {
  const _TaskActionsCard({required this.enabled, required this.onCreate});

  final bool enabled;
  final Future<void> Function(String taskType, String prompt) onCreate;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '发起任务',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 12,
              runSpacing: 12,
              children: [
                FilledButton.icon(
                  onPressed: enabled
                      ? () => onCreate('object_recognition', '')
                      : null,
                  icon: const Icon(Icons.search_rounded),
                  label: const Text('识物'),
                ),
                FilledButton.icon(
                  onPressed: enabled
                      ? () => onCreate('picture_book_recognition', '')
                      : null,
                  icon: const Icon(Icons.auto_stories_outlined),
                  label: const Text('绘本识别'),
                ),
                FilledButton.icon(
                  onPressed: enabled
                      ? () => _promptForQuestion(context)
                      : null,
                  icon: const Icon(Icons.question_answer_outlined),
                  label: const Text('拍照问答'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _promptForQuestion(BuildContext context) async {
    final controller = TextEditingController();
    final question = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('拍照问答'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 200,
          decoration: const InputDecoration(
            labelText: '想问的问题',
            hintText: '例如：这是什么',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () =>
                Navigator.of(dialogContext).pop(controller.text.trim()),
            child: const Text('发送'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (question == null || question.isEmpty) {
      return;
    }
    await onCreate('photo_qa', question);
  }
}

class _TasksCard extends StatelessWidget {
  const _TasksCard({required this.tasks, required this.onCancel});

  final List<VisionTask> tasks;
  final Future<void> Function(String taskId) onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 20, 20, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '最近任务',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            if (tasks.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  '还没有发起过任务。',
                  style: theme.textTheme.bodyMedium,
                ),
              )
            else
              ...tasks.take(8).map(
                (task) => ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(visionTaskTypeLabel(task.taskType)),
                  subtitle: Text(visionStatusLabel(task.status)),
                  trailing: task.isPending
                      ? IconButton(
                          tooltip: '取消任务',
                          onPressed: () => onCancel(task.taskId),
                          icon: const Icon(Icons.close_rounded),
                        )
                      : null,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _ResultsCard extends StatelessWidget {
  const _ResultsCard({required this.results});

  final List<VisionResult> results;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '识别结果',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            if (results.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  '暂无可查看的结果。结果会在保留期后自动删除。',
                  style: theme.textTheme.bodyMedium,
                ),
              )
            else
              ...results.take(8).map((result) {
                final labels = result.labels
                    .map((label) => label.label)
                    .where((label) => label.isNotEmpty)
                    .take(5)
                    .join('、');
                final summary = result.summary.isNotEmpty
                    ? result.summary
                    : labels;
                return ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.image_search_rounded),
                  title: Text(visionTaskTypeLabel(result.taskType)),
                  subtitle: Text(
                    summary.isEmpty ? '已完成' : summary,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                );
              }),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({
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
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.error_outline_rounded, size: 40),
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
