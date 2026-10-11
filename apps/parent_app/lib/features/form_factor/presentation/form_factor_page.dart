import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/form_factor_controller.dart';
import '../domain/form_factor_profile.dart';

/// Guardian, read-only view of one device's composed form-factor profile.
///
/// A form factor is named technically and never binds to a character or an
/// appearance. The page shows the capabilities a body reports and the actions
/// the platform composed from them.
class FormFactorPage extends ConsumerWidget {
  const FormFactorPage({required this.deviceId, super.key});

  final String deviceId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final capability = ref.watch(formFactorCapabilityProvider(deviceId));
    return Scaffold(
      appBar: AppBar(
        title: const Text('形态能力'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: capability.when(
          data: (value) => value.supported
              ? 'form-factor-data'
              : 'form-factor-unsupported',
          error: (_, _) => 'form-factor-error',
          loading: () => 'form-factor-loading',
        ),
        child: capability.when(
          loading: () => const Center(
            key: ValueKey<String>('form-factor-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('form-factor-error'),
            onRetry: () =>
                ref.invalidate(formFactorCapabilityProvider(deviceId)),
          ),
          data: (value) {
            if (!value.supported) {
              return const Center(
                key: ValueKey<String>('form-factor-unsupported'),
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text('设备尚未上报形态能力。'),
                ),
              );
            }
            return _ProfileBody(
              key: const ValueKey<String>('form-factor-data'),
              capability: value,
            );
          },
        ),
      ),
    );
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/devices');
  }
}

class _ProfileBody extends StatelessWidget {
  const _ProfileBody({required this.capability, super.key});

  final FormFactorCapability capability;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        AppReveal(
          child: Card(
            child: ListTile(
              leading: const Icon(Icons.category_outlined),
              title: Text(_nameLabel(capability.technicalName)),
              subtitle: Text('技术名称：${capability.technicalName}'),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: _BitCard(
            title: '硬件能力',
            emptyText: '设备没有上报硬件能力。',
            bits: capability.capabilities,
            labelBuilder: _capabilityLabel,
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _BitCard(
            title: '可用动作',
            emptyText: '设备没有可用的动作。',
            bits: capability.actions,
            labelBuilder: _actionLabel,
          ),
        ),
      ],
    );
  }
}

class _BitCard extends StatelessWidget {
  const _BitCard({
    required this.title,
    required this.emptyText,
    required this.bits,
    required this.labelBuilder,
  });

  final String title;
  final String emptyText;
  final List<FormFactorBit> bits;
  final String Function(String key) labelBuilder;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 12),
            if (bits.isEmpty)
              Text(emptyText)
            else
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final bit in bits)
                    Chip(label: Text(labelBuilder(bit.key))),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

String _nameLabel(String name) {
  return (
    {
      'desktop': '桌面形态',
      'moving_body': '可移动形态',
      'wearable': '可穿戴形态',
    }[name] ?? '自定义形态');
}

String _capabilityLabel(String key) {
  return (
    {
      'display': '屏幕',
      'camera': '摄像头',
      'microphone': '麦克风',
      'speaker': '扬声器',
      'touch': '触摸',
      'locomotion': '行走能力',
      'servo': '舵机',
      'cellular': '蜂窝网络',
      'battery': '电池',
      'wearable': '可穿戴',
    }[key] ?? key);
}

String _actionLabel(String key) {
  return (
    {
      'expression': '表情',
      'video_display': '视频显示',
      'touch': '触摸交互',
      'walk': '行走',
      'dance': '舞蹈',
      'tail': '尾巴动作',
      'gesture': '手势',
      'voice': '语音对话',
      'video_call': '视频通话',
      'location': '定位',
    }[key] ?? key);
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.onRetry, super.key});

  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('形态能力暂时无法加载', textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重试')),
          ],
        ),
      ),
    );
  }
}
