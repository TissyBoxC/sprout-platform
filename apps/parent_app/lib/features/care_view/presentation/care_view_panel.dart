import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/app_motion.dart';
import '../application/care_view_controller.dart';
import '../domain/care_view.dart';
import 'care_view_copy.dart';

/// The guardian's live view of one device.
///
/// The window states what the device is doing right now: waiting for the device
/// to confirm, showing pictures, or no longer looking. Once the look stops it
/// stops showing pictures in the same build, because a frozen picture looks
/// exactly like a live camera over a child.
class CareViewPanel extends ConsumerWidget {
  const CareViewPanel({
    super.key,
    required this.deviceId,
    this.deviceName = '',
  });

  final String deviceId;
  final String deviceName;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(careViewControllerProvider(deviceId));
    final view = state.view;
    if (view == null) {
      return _IdleView(
        deviceId: deviceId,
        deviceName: deviceName,
        failure: state.error,
        isLoading: state.isLoading,
      );
    }
    return _LiveView(
      deviceId: deviceId,
      view: view,
      frame: state.latestFrame,
    );
  }
}

/// Shows the one-time notice a guardian must accept before a look.
///
/// The notice names what the child sees and hears and says the child can stop
/// the look. Accepting is the guardian's own act, so the start button alone can
/// never open the camera.
Future<bool> confirmCareView(
  BuildContext context, {
  required String deviceName,
}) async {
  final label = deviceName.isEmpty ? '设备' : deviceName;
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      icon: const Icon(Icons.visibility_outlined),
      title: const Text(CareViewCopy.confirmTitle),
      content: Text('$label${CareViewCopy.confirmBody}'),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text(CareViewCopy.cancelStartButton),
        ),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text(CareViewCopy.startButton),
        ),
      ],
    ),
  );
  return confirmed ?? false;
}

/// Confirms with the guardian, then asks the device.
///
/// Shared by the device sheet, the full page, and a restart, so a shorter entry
/// path can never skip the notice.
Future<void> startCareView(
  BuildContext context,
  WidgetRef ref,
  String deviceId, {
  String deviceName = '',
}) async {
  final controller = ref.read(careViewControllerProvider(deviceId).notifier);
  final confirmed = await confirmCareView(context, deviceName: deviceName);
  if (!confirmed) {
    return;
  }
  controller.reset();
  try {
    await controller.start();
  } on Object {
    // The controller already stores the failure and the idle card renders it, so
    // the reason is shown once. A snack bar as well would say it twice and would
    // disappear before a guardian had finished reading.
  }
}

class _IdleView extends ConsumerWidget {
  const _IdleView({
    required this.deviceId,
    required this.deviceName,
    required this.failure,
    required this.isLoading,
  });

  final String deviceId;
  final String deviceName;
  final Object? failure;
  final bool isLoading;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final label = deviceName.isEmpty ? '这台设备' : deviceName;
    return _Shell(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const _HeadlineRow(
            icon: Icons.visibility_outlined,
            label: CareViewCopy.entryTitle,
          ),
          const SizedBox(height: 6),
          Text('看看$label此刻在做什么。${CareViewCopy.entrySummary}'),
          if (failure != null) ...[
            const SizedBox(height: 8),
            _FailureNotice(message: careViewMessageFor(failure)),
          ],
          const SizedBox(height: 12),
          FilledButton(
            onPressed: isLoading
                ? null
                : () => startCareView(
                    context,
                    ref,
                    deviceId,
                    deviceName: deviceName,
                  ),
            child: Text(
              isLoading ? CareViewCopy.startingButton : CareViewCopy.startButton,
            ),
          ),
        ],
      ),
    );
  }
}

class _LiveView extends ConsumerStatefulWidget {
  const _LiveView({
    required this.deviceId,
    required this.view,
    required this.frame,
  });

  final String deviceId;
  final CareView view;
  final Uint8List? frame;

  @override
  ConsumerState<_LiveView> createState() => _LiveViewState();
}

class _LiveViewState extends ConsumerState<_LiveView> {
  @override
  void initState() {
    super.initState();
    // The window owns the picture connection while it is on screen and drops it
    // when it leaves, so no reception keeps running behind a closed page.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        ref
            .read(careViewControllerProvider(widget.deviceId).notifier)
            .openStream();
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final view = widget.view;
    final controller = ref.read(
      careViewControllerProvider(widget.deviceId).notifier,
    );
    final isOpen = view.isRequested || view.isStreaming;
    return _Shell(
      accent: view.canShowPicture ? const Color(0xFF4CAF50) : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          _Headline(view: view),
          const SizedBox(height: 8),
          _Picture(view: view, frame: widget.frame),
          const SizedBox(height: 8),
          _Detail(view: view),
          const SizedBox(height: 12),
          if (isOpen)
            Wrap(
              spacing: 12,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: <Widget>[
                _Countdown(deadline: view.deadline),
                FilledButton(
                  onPressed: controller.end,
                  child: const Text(CareViewCopy.endButton),
                ),
              ],
            )
          else
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: <Widget>[
                FilledButton(
                  onPressed: () => startCareView(
                    context,
                    ref,
                    widget.deviceId,
                  ),
                  child: const Text(CareViewCopy.startButton),
                ),
                TextButton(
                  onPressed: controller.reset,
                  child: const Text(CareViewCopy.backButton),
                ),
              ],
            ),
          if (view.canShowPicture) ...[
            const SizedBox(height: 8),
            const Text(CareViewCopy.indicationReminder),
          ],
        ],
      ),
    );
  }
}

class _Headline extends StatelessWidget {
  const _Headline({required this.view});

  final CareView view;

  @override
  Widget build(BuildContext context) {
    return switch (view.state) {
      CareViewState.requested => const _HeadlineRow(
        icon: Icons.hourglass_top_rounded,
        label: CareViewCopy.requestedTitle,
      ),
      CareViewState.streaming => _HeadlineRow(
        icon: view.isStreamUnavailable
            ? Icons.cloud_off_outlined
            : Icons.videocam_rounded,
        label: view.isStreamUnavailable
            ? CareViewCopy.streamUnavailableTitle
            : CareViewCopy.streamingTitle,
        color: view.isStreamUnavailable ? null : const Color(0xFF2E7D32),
      ),
      CareViewState.ended ||
      CareViewState.declined ||
      CareViewState.failed => _HeadlineRow(
        icon: Icons.stop_circle_outlined,
        label: CareViewCopy.finishedTitle(view),
      ),
      CareViewState.unknown => const _HeadlineRow(
        icon: Icons.info_outline,
        label: CareViewCopy.unknownStateTitle,
      ),
    };
  }
}

class _HeadlineRow extends StatelessWidget {
  const _HeadlineRow({required this.icon, required this.label, this.color});

  final IconData icon;
  final String label;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: <Widget>[
        Icon(icon, size: 18, color: color),
        const SizedBox(width: 6),
        Expanded(
          child: Text(
            label,
            style: theme.textTheme.titleSmall?.copyWith(color: color),
          ),
        ),
      ],
    );
  }
}

class _Detail extends StatelessWidget {
  const _Detail({required this.view});

  final CareView view;

  @override
  Widget build(BuildContext context) {
    final text = switch (view.state) {
      CareViewState.requested => CareViewCopy.requestedBody,
      CareViewState.streaming => view.isStreamUnavailable
          ? CareViewCopy.streamUnavailableBody
          : null,
      CareViewState.ended ||
      CareViewState.declined ||
      CareViewState.failed => CareViewCopy.finishedDetail(view),
      CareViewState.unknown => CareViewCopy.unknownStateBody,
    };
    if (text == null) {
      return const SizedBox.shrink();
    }
    return Text(text, style: Theme.of(context).textTheme.bodyMedium);
  }
}

/// The picture area.
///
/// It separates the situations a guardian must be able to tell apart: the
/// device is still being asked, it is preparing its picture, no picture can be
/// delivered, and a picture arrived. An unexplained empty box is never shown.
class _Picture extends StatelessWidget {
  const _Picture({required this.view, required this.frame});

  final CareView view;
  final Uint8List? frame;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final Widget content;
    if (view.isRequested) {
      content = const _Placeholder(
        icon: Icons.notifications_active_outlined,
        title: CareViewCopy.requestedTitle,
        detail: CareViewCopy.requestedBody,
      );
    } else if (!view.isStreaming) {
      content = _Placeholder(
        icon: Icons.no_photography_outlined,
        title: CareViewCopy.finishedTitle(view),
        detail: CareViewCopy.unknownStateBody,
      );
    } else if (view.isStreamUnavailable) {
      content = const _Placeholder(
        icon: Icons.cloud_off_outlined,
        title: CareViewCopy.streamUnavailableTitle,
        detail: CareViewCopy.streamUnavailableBody,
      );
    } else if (frame == null) {
      content = const _Placeholder(
        icon: Icons.photo_camera_back_outlined,
        title: CareViewCopy.preparingTitle,
        detail: CareViewCopy.picturePending,
      );
    } else {
      content = Image.memory(
        frame!,
        fit: BoxFit.contain,
        gaplessPlayback: true,
        semanticLabel: CareViewCopy.pictureLabel,
        errorBuilder: (_, _, _) => const _Placeholder(
          icon: Icons.cloud_off_outlined,
          title: CareViewCopy.streamUnavailableTitle,
          detail: CareViewCopy.pictureBroken,
        ),
      );
    }

    return ClipRRect(
      borderRadius: BorderRadius.circular(16),
      child: Container(
        width: double.infinity,
        constraints: const BoxConstraints(minHeight: 180),
        color: const Color(0xFFFFF2F5),
        alignment: Alignment.center,
        child: AnimatedSwitcher(
          duration: AppMotion.fast,
          child: KeyedSubtree(
            key: ValueKey<String>(_stateKey(hasPicture: frame != null)),
            child: DefaultTextStyle(
              style: theme.textTheme.bodyMedium ?? const TextStyle(),
              textAlign: TextAlign.center,
              child: content,
            ),
          ),
        ),
      ),
    );
  }

  String _stateKey({required bool hasPicture}) {
    if (view.isRequested) {
      return 'waiting';
    }
    if (!view.isStreaming) {
      return 'stopped';
    }
    if (view.isStreamUnavailable) {
      return 'unavailable';
    }
    return hasPicture ? 'picture' : 'preparing';
  }
}

class _Placeholder extends StatelessWidget {
  const _Placeholder({
    required this.icon,
    required this.title,
    required this.detail,
  });

  final IconData icon;
  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Icon(icon, size: 34, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(height: 10),
          Text(
            title,
            textAlign: TextAlign.center,
            style: theme.textTheme.titleSmall,
          ),
          const SizedBox(height: 6),
          Text(
            detail,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall,
          ),
        ],
      ),
    );
  }
}

/// Counts down the remaining time of one look.
///
/// The countdown is the guardian's only cue that the look stops by itself, so
/// it ticks every second and never runs below zero.
class _Countdown extends StatefulWidget {
  const _Countdown({required this.deadline});

  final DateTime deadline;

  @override
  State<_Countdown> createState() => _CountdownState();
}

class _CountdownState extends State<_Countdown> {
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        setState(() {});
      }
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final remaining = widget.deadline.difference(DateTime.now().toUtc());
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        const Icon(Icons.timer_outlined, size: 18),
        const SizedBox(width: 4),
        Text(CareViewCopy.remaining(remaining.inSeconds)),
      ],
    );
  }
}

class _FailureNotice extends StatelessWidget {
  const _FailureNotice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: const Color(0xFFFFF2F5),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: const Color(0xFFF0BDCB)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(
            Icons.info_outline,
            size: 18,
            color: theme.colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 8),
          Expanded(child: Text(message)),
        ],
      ),
    );
  }
}

class _Shell extends StatelessWidget {
  const _Shell({required this.child, this.accent});

  final Widget child;
  final Color? accent;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: accent == null
            ? const Color(0xFFFFF9FA)
            : const Color(0xFFF4FBF4),
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: accent ?? const Color(0xFFF7D8E2)),
      ),
      child: child,
    );
  }
}
