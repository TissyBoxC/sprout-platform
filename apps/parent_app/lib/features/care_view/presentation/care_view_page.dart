import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../application/care_view_controller.dart';
import 'care_view_copy.dart';
import 'care_view_panel.dart';

/// The guardian's dedicated live-view screen.
///
/// Leaving this screen always ends an open look: the child's device keeps its
/// indication and camera on until it is told to stop, so navigating away must
/// be treated as "结束查看", not as "hide the window".
class CareViewPage extends ConsumerWidget {
  const CareViewPage({
    super.key,
    required this.deviceId,
    this.deviceName = '',
  });

  final String deviceId;
  final String deviceName;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return PopScope<Object?>(
      // A look in progress is stopped first so the screen can show it did stop.
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) {
          return;
        }
        _leave(context, ref);
      },
      child: Scaffold(
        appBar: AppBar(
          title: Text(
            deviceName.isEmpty
                ? CareViewCopy.pageTitle
                : '${CareViewCopy.pageTitle} · $deviceName',
          ),
          leading: IconButton(
            tooltip: CareViewCopy.backButton,
            onPressed: () => _leave(context, ref),
            icon: const Icon(Icons.arrow_back_rounded),
          ),
        ),
        body: ListView(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 32),
          children: <Widget>[
            const _ReadBeforeCard(),
            CareViewPanel(deviceId: deviceId, deviceName: deviceName),
          ],
        ),
      ),
    );
  }

  Future<void> _leave(BuildContext context, WidgetRef ref) async {
    final view = ref.read(careViewControllerProvider(deviceId)).view;
    final controller = ref.read(careViewControllerProvider(deviceId).notifier);
    if (view != null && !view.isFinished) {
      // Ending uses the stored view, which is what actually stops the camera.
      await controller.end();
    }
    controller.reset();
    if (!context.mounted) {
      return;
    }
    if (context.canPop()) {
      context.pop();
    } else {
      context.go('/devices');
    }
  }
}

/// Restates the promise before any picture appears.
class _ReadBeforeCard extends StatelessWidget {
  const _ReadBeforeCard();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(
                  Icons.info_outline,
                  size: 18,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 6),
                Text('查看前请知道', style: theme.textTheme.titleSmall),
              ],
            ),
            const SizedBox(height: 8),
            const _Bullet('设备会显示提示并播放提示音，孩子知道正在被查看。'),
            const _Bullet('孩子可以按键随时关闭，关闭后画面会立即停止。'),
            const _Bullet('一次查看最多 5 分钟，到时间会自动结束。'),
          ],
        ),
      ),
    );
  }
}

class _Bullet extends StatelessWidget {
  const _Bullet(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const Text('· '),
          Expanded(child: Text(text)),
        ],
      ),
    );
  }
}
