import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../device/application/device_binding_controller.dart';
import '../../device/data/device_binding_api.dart';
import '../../video_companion/application/video_companion_controller.dart';
import '../application/video_session_controller.dart';
import '../domain/video_session.dart';
import 'video_session_copy.dart';

/// The guardian's 视频通话 screen.
///
/// It is a settings screen, not a dialer: the platform owns one family rule per
/// family that says whether a call may happen, which permissions the device
/// holds, what the child is asked, the worst picture quality allowed, the ring
/// window, and the hard ceiling on one call. The screen also shows what each
/// bound device last reported, because the guardian's real question is "is this
/// permission actually in effect on the robot".
class VideoSessionPage extends ConsumerWidget {
  const VideoSessionPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(videoSessionControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text(VideoSessionCopy.pageTitle),
        leading: IconButton(
          tooltip: VideoSessionCopy.backButton,
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: _stateKey(state),
        child: _body(context, ref, state),
      ),
    );
  }

  String _stateKey(VideoSessionState state) {
    if (state.error != null && !state.hasPolicy) {
      return 'video-session-error';
    }
    if (state.isLoading && !state.hasPolicy) {
      return 'video-session-loading';
    }
    return 'video-session-content';
  }

  Widget _body(BuildContext context, WidgetRef ref, VideoSessionState state) {
    if (state.error != null && !state.hasPolicy) {
      return _ErrorState(
        key: const ValueKey<String>('video-session-error'),
        message: videoSessionMessageFor(state.error),
        onRetry: () => ref.read(videoSessionControllerProvider.notifier).refresh(),
      );
    }
    final policy = state.policy;
    if (state.isLoading && policy == null) {
      return const _LoadingState();
    }
    if (policy == null) {
      return const _LoadingState();
    }
    return _VideoSessionBody(state: state, policy: policy);
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

/// The editable form, the device card and the save action.
class _VideoSessionBody extends ConsumerStatefulWidget {
  const _VideoSessionBody({required this.state, required this.policy});

  final VideoSessionState state;
  final VideoSessionPolicy policy;

  @override
  ConsumerState<_VideoSessionBody> createState() => _VideoSessionBodyState();
}

class _VideoSessionBodyState extends ConsumerState<_VideoSessionBody> {
  late VideoSessionPolicy _draft;

  @override
  void initState() {
    super.initState();
    _draft = widget.policy;
  }

  @override
  void didUpdateWidget(covariant _VideoSessionBody oldWidget) {
    super.didUpdateWidget(oldWidget);
    // A stored revision that changed underneath the form replaces the draft,
    // so the guardian never saves one revision on top of another.
    if (oldWidget.policy.revision != widget.policy.revision) {
      _draft = widget.policy;
    }
  }

  bool get _hasChanges => !_samePolicy(_draft, widget.policy);

  bool _samePolicy(VideoSessionPolicy a, VideoSessionPolicy b) {
    return a.enabled == b.enabled &&
        a.cameraAllowed == b.cameraAllowed &&
        a.microphoneAllowed == b.microphoneAllowed &&
        a.requireChildConsent == b.requireChildConsent &&
        a.qualityFloor == b.qualityFloor &&
        a.ringTimeoutSeconds == b.ringTimeoutSeconds &&
        a.maxSessionSeconds == b.maxSessionSeconds;
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final devices = ref.watch(deviceBindingControllerProvider).value?.bindings;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
      children: <Widget>[
        AppReveal(child: _IntroCard()),
        const SizedBox(height: 12),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: _PolicyCard(
            draft: _draft,
            qualities: state.qualities,
            onChanged: (next) => setState(() => _draft = next),
            onConsentBlocked: _confirmConsentOff,
          ),
        ),
        const SizedBox(height: 12),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _DeviceCard(
            state: state,
            devices: devices ?? const <BoundDevice>[],
            onRetry: () => ref
                .read(videoSessionControllerProvider.notifier)
                .refreshDevices(),
          ),
        ),
        const SizedBox(height: 20),
        _SaveSection(
          isSaving: state.isSaving,
          hasChanges: _hasChanges,
          notice: state.notice,
          onSave: () => ref
              .read(videoSessionControllerProvider.notifier)
              .save(_draft),
        ),
      ],
    );
  }

  /// Turning the child confirmation off changes what happens on the device
  /// without the child seeing it first, so it is confirmed explicitly.
  Future<bool> _confirmConsentOff() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text(VideoSessionCopy.consentOffConfirmTitle),
        content: const Text(VideoSessionCopy.consentOffConfirmBody),
        actions: <Widget>[
          TextButton(
            key: const ValueKey<String>('video-session-consent-cancel'),
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text(VideoSessionCopy.consentOffCancelAction),
          ),
          FilledButton(
            key: const ValueKey<String>('video-session-consent-confirm'),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text(VideoSessionCopy.consentOffConfirmAction),
          ),
        ],
      ),
    );
    return confirmed == true;
  }
}

/// What this screen decides, and what it deliberately does not.
class _IntroCard extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(VideoSessionCopy.introTitle, style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(VideoSessionCopy.introBody, style: theme.textTheme.bodyMedium),
            const SizedBox(height: 12),
            _PrivacyLine(text: VideoSessionCopy.privacyBody),
            const SizedBox(height: 6),
            _PrivacyLine(text: VideoSessionCopy.noRecordingBody),
          ],
        ),
      ),
    );
  }
}

/// One privacy promise, marked with a small lock rather than a card of its own.
class _PrivacyLine extends StatelessWidget {
  const _PrivacyLine({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        const Icon(
          Icons.lock_outline_rounded,
          size: 16,
          color: Color(0xFF6B4F5A),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(text, style: Theme.of(context).textTheme.bodySmall),
        ),
      ],
    );
  }
}

/// The permission switches, the quality floor and the two durations.
class _PolicyCard extends StatelessWidget {
  const _PolicyCard({
    required this.draft,
    required this.qualities,
    required this.onChanged,
    required this.onConsentBlocked,
  });

  final VideoSessionPolicy draft;
  final List<VideoQualityFloor> qualities;
  final ValueChanged<VideoSessionPolicy> onChanged;

  /// Called when the guardian turns the child confirmation off. The switch only
  /// moves after the returned future resolves true.
  final Future<bool> Function() onConsentBlocked;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Card(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(8, 8, 8, 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Padding(
                  padding: const EdgeInsets.fromLTRB(12, 12, 12, 4),
                  child: Text(
                    VideoSessionCopy.policyTitle,
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                SwitchListTile(
                  key: const ValueKey<String>('video-session-enabled'),
                  title: const Text(VideoSessionCopy.enabledTitle),
                  subtitle: const Text(VideoSessionCopy.enabledBody),
                  value: draft.enabled,
                  onChanged: (value) => onChanged(draft.copyWith(enabled: value)),
                ),
                SwitchListTile(
                  key: const ValueKey<String>('video-session-camera'),
                  title: const Text(VideoSessionCopy.cameraTitle),
                  subtitle: const Text(VideoSessionCopy.cameraBody),
                  value: draft.cameraAllowed,
                  onChanged: (value) =>
                      onChanged(draft.copyWith(cameraAllowed: value)),
                ),
                SwitchListTile(
                  key: const ValueKey<String>('video-session-microphone'),
                  title: const Text(VideoSessionCopy.microphoneTitle),
                  subtitle: const Text(VideoSessionCopy.microphoneBody),
                  value: draft.microphoneAllowed,
                  onChanged: (value) =>
                      onChanged(draft.copyWith(microphoneAllowed: value)),
                ),
                SwitchListTile(
                  key: const ValueKey<String>('video-session-consent'),
                  title: const Text(VideoSessionCopy.consentTitle),
                  subtitle: const Text(VideoSessionCopy.consentBody),
                  value: draft.requireChildConsent,
                  onChanged: (value) {
                    if (value) {
                      onChanged(draft.copyWith(requireChildConsent: true));
                      return;
                    }
                    _toggleConsentOff();
                  },
                ),
                if (draft.isCaptureBlocked)
                  Padding(
                    padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
                    child: Text(
                      VideoSessionCopy.captureBlockedHint,
                      key: const ValueKey<String>('video-session-capture-blocked'),
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 12),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  VideoSessionCopy.qualityTitle,
                  style: theme.textTheme.titleSmall,
                ),
                const SizedBox(height: 4),
                Text(
                  VideoSessionCopy.qualityBody,
                  style: theme.textTheme.bodySmall,
                ),
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: <Widget>[
                    for (final quality in qualities)
                      ChoiceChip(
                        key: ValueKey<String>(
                          'video-session-quality-${quality.wireValue}',
                        ),
                        label: Text(VideoSessionCopy.qualityName(quality)),
                        selected: draft.qualityFloor == quality,
                        onSelected: (_) =>
                            onChanged(draft.copyWith(qualityFloor: quality)),
                      ),
                  ],
                ),
                const SizedBox(height: 6),
                Text(
                  VideoSessionCopy.qualityHint(draft.qualityFloor),
                  style: theme.textTheme.bodySmall,
                ),
                const SizedBox(height: 20),
                Text(
                  VideoSessionCopy.ringTitle,
                  style: theme.textTheme.titleSmall,
                ),
                const SizedBox(height: 4),
                Text(
                  VideoSessionCopy.ringBody,
                  style: theme.textTheme.bodySmall,
                ),
                _Stepper(
                  keyPrefix: 'video-session-ring',
                  value: VideoSessionCopy.seconds(draft.ringTimeoutSeconds),
                  onDecrease: draft.ringTimeoutSeconds >
                          VideoSessionPolicy.minimumRingTimeoutSeconds
                      ? () => onChanged(
                          draft.copyWith(
                            ringTimeoutSeconds: _stepRing(
                              draft.ringTimeoutSeconds,
                              -1,
                            ),
                          ),
                        )
                      : null,
                  onIncrease: draft.ringTimeoutSeconds <
                          VideoSessionPolicy.maximumRingTimeoutSeconds
                      ? () => onChanged(
                          draft.copyWith(
                            ringTimeoutSeconds: _stepRing(
                              draft.ringTimeoutSeconds,
                              1,
                            ),
                          ),
                        )
                      : null,
                ),
                const SizedBox(height: 16),
                Text(
                  VideoSessionCopy.durationTitle,
                  style: theme.textTheme.titleSmall,
                ),
                const SizedBox(height: 4),
                Text(
                  VideoSessionCopy.durationBody,
                  style: theme.textTheme.bodySmall,
                ),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: <Widget>[
                    for (final minutes in const <int>[10, 30, 60, 120])
                      ChoiceChip(
                        key: ValueKey<String>('video-session-duration-$minutes'),
                        label: Text('$minutes 分钟'),
                        selected:
                            draft.maxSessionSeconds == minutes * 60,
                        onSelected: (_) => onChanged(
                          draft.copyWith(maxSessionSeconds: minutes * 60),
                        ),
                      ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _toggleConsentOff() async {
    final confirmed = await onConsentBlocked();
    if (!confirmed) {
      return;
    }
    onChanged(draft.copyWith(requireChildConsent: false));
  }

  /// Moves the ring window in 5 second steps, which is the granularity the
  /// platform's own bounds are expressed in.
  int _stepRing(int current, int direction) {
    const step = 5;
    return (current + direction * step).clamp(
      VideoSessionPolicy.minimumRingTimeoutSeconds,
      VideoSessionPolicy.maximumRingTimeoutSeconds,
    );
  }
}

/// One bounded number with a decrement and an increment control.
class _Stepper extends StatelessWidget {
  const _Stepper({
    required this.keyPrefix,
    required this.value,
    required this.onDecrease,
    required this.onIncrease,
  });

  final String keyPrefix;
  final String value;
  final VoidCallback? onDecrease;
  final VoidCallback? onIncrease;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: <Widget>[
        IconButton(
          key: ValueKey<String>('$keyPrefix-decrease'),
          tooltip: '缩短响铃',
          onPressed: onDecrease,
          icon: const Icon(Icons.remove_circle_outline_rounded),
        ),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
          decoration: BoxDecoration(
            color: theme.colorScheme.surfaceContainerLowest,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(color: theme.colorScheme.outlineVariant),
          ),
          child: Text(
            value,
            key: ValueKey<String>('$keyPrefix-value'),
            style: theme.textTheme.titleSmall,
          ),
        ),
        IconButton(
          key: ValueKey<String>('$keyPrefix-increase'),
          tooltip: '延长响铃',
          onPressed: onIncrease,
          icon: const Icon(Icons.add_circle_outline_rounded),
        ),
      ],
    );
  }
}

/// What each bound device last reported, in the guardian's own terms.
class _DeviceCard extends StatelessWidget {
  const _DeviceCard({
    required this.state,
    required this.devices,
    required this.onRetry,
  });

  final VideoSessionState state;
  final List<BoundDevice> devices;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      key: const ValueKey<String>('video-session-devices'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(VideoSessionCopy.deviceTitle, style: theme.textTheme.titleMedium),
            const SizedBox(height: 10),
            if (state.isLoadingDevices && !state.hasDeviceReport)
              Text(VideoSessionCopy.loading, style: theme.textTheme.bodySmall)
            else if (state.devicesError != null && !state.hasDeviceReport)
              Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    VideoSessionCopy.devicesFailed,
                    key: const ValueKey<String>('video-session-devices-error'),
                    style: theme.textTheme.bodySmall,
                  ),
                  const SizedBox(height: 4),
                  OutlinedButton(
                    onPressed: onRetry,
                    child: const Text(VideoSessionCopy.devicesRetryButton),
                  ),
                ],
              )
            else if (!state.hasDeviceReport)
              Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    VideoSessionCopy.deviceEmptyTitle,
                    key: const ValueKey<String>('video-session-devices-empty'),
                    style: theme.textTheme.titleSmall,
                  ),
                  const SizedBox(height: 4),
                  Text(
                    VideoSessionCopy.deviceEmptyBody,
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              )
            else
              for (final device in state.devices)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: _DeviceRow(device: device, name: _nameOf(device)),
                ),
          ],
        ),
      ),
    );
  }

  /// The device's family-visible name, falling back to a stable neutral label
  /// rather than the raw id.
  String _nameOf(VideoSessionDevice device) {
    for (final bound in devices) {
      if (bound.deviceId == device.deviceId && bound.deviceName.isNotEmpty) {
        return bound.deviceName;
      }
    }
    return '已绑定的设备';
  }
}

class _DeviceRow extends ConsumerWidget {
  const _DeviceRow({required this.device, required this.name});

  final VideoSessionDevice device;
  final String name;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    // The stored policy says what the guardian allowed; the capability says what
    // the device can do right now. A device that dropped off the network still
    // holds an enabled policy, so showing only the policy would promise a call
    // that cannot connect.
    final capability = ref.watch(videoCapabilityProvider(device.deviceId));
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(name, style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          Text(_readinessText(device), style: theme.textTheme.bodySmall),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              _StatusPill(
                icon: device.cameraAllowed
                    ? Icons.videocam_rounded
                    : Icons.videocam_off_outlined,
                label: device.cameraAllowed
                    ? VideoSessionCopy.cameraOn
                    : VideoSessionCopy.cameraOff,
              ),
              _StatusPill(
                icon: device.microphoneAllowed
                    ? Icons.mic_rounded
                    : Icons.mic_off_outlined,
                label: device.microphoneAllowed
                    ? VideoSessionCopy.microphoneOn
                    : VideoSessionCopy.microphoneOff,
              ),
              _StatusPill(
                icon: _callIcon(device.callState),
                label: _callText(device),
              ),
            ],
          ),
          capability.maybeWhen(
            data: (value) => value.supported
                ? const SizedBox.shrink()
                : Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Text(
                      VideoSessionCopy.capabilityUnsupported,
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
            orElse: () => const SizedBox.shrink(),
          ),
          if (device.reportedAt != null) ...[
            const SizedBox(height: 8),
            Text(
              '${VideoSessionCopy.reportedAt}：'
              '${VideoSessionCopy.dateTime(device.reportedAt!)}',
              style: theme.textTheme.bodySmall,
            ),
          ],
        ],
      ),
    );
  }

  String _readinessText(VideoSessionDevice device) {
    return switch (device.readiness) {
      VideoSessionReadiness.unsupported => VideoSessionCopy.deviceUnsupported,
      VideoSessionReadiness.ready => VideoSessionCopy.deviceReady,
      VideoSessionReadiness.outdated => VideoSessionCopy.deviceOutdated,
      // A device that appears in the list already reported, so the unreported
      // sentence is never a stand-in for an empty state here.
      VideoSessionReadiness.unreported => VideoSessionCopy.deviceUnreported,
    };
  }

  String _callText(VideoSessionDevice device) {
    final state = switch (device.callState) {
      VideoCallState.idle => VideoSessionCopy.callIdle,
      VideoCallState.ringing => VideoSessionCopy.callRinging,
      VideoCallState.active => VideoSessionCopy.callActive,
      VideoCallState.ended => VideoSessionCopy.callEnded,
      VideoCallState.rejected => VideoSessionCopy.callRejected,
      VideoCallState.failed => VideoSessionCopy.callFailed,
      VideoCallState.unknown => VideoSessionCopy.callUnknown,
    };
    if (device.isInCall || device.lastEndReason == VideoEndReason.none) {
      return state;
    }
    return '$state · ${_endReasonText(device.lastEndReason)}';
  }

  String _endReasonText(VideoEndReason reason) {
    return switch (reason) {
      VideoEndReason.hangup => VideoSessionCopy.endHangup,
      VideoEndReason.rejected => VideoSessionCopy.endRejected,
      VideoEndReason.timeout => VideoSessionCopy.endTimeout,
      VideoEndReason.network => VideoSessionCopy.endNetwork,
      VideoEndReason.permissionRevoked =>
        VideoSessionCopy.endPermissionRevoked,
      VideoEndReason.none || VideoEndReason.other => VideoSessionCopy.endOther,
    };
  }

  IconData _callIcon(VideoCallState state) {
    return switch (state) {
      VideoCallState.active => Icons.videocam_rounded,
      VideoCallState.ringing => Icons.phone_in_talk_outlined,
      VideoCallState.rejected || VideoCallState.failed =>
        Icons.videocam_off_outlined,
      _ => Icons.call_end_rounded,
    };
  }
}

/// One reported fact, as a small outlined pill.
class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Icon(icon, size: 14, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(width: 6),
          Text(label, style: theme.textTheme.labelSmall),
        ],
      ),
    );
  }
}

/// The save button and the sentence that follows it.
class _SaveSection extends StatelessWidget {
  const _SaveSection({
    required this.isSaving,
    required this.hasChanges,
    required this.notice,
    required this.onSave,
  });

  final bool isSaving;
  final bool hasChanges;
  final VideoSessionNotice? notice;
  final Future<bool> Function() onSave;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        FilledButton(
          key: const ValueKey<String>('video-session-save'),
          onPressed: isSaving || !hasChanges ? null : () => onSave(),
          child: Text(
            isSaving
                ? VideoSessionCopy.savingButton
                : VideoSessionCopy.saveButton,
          ),
        ),
        const SizedBox(height: 8),
        if (isSaving)
          Text(
            VideoSessionCopy.savingButton,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall,
          )
        else if (!hasChanges && notice == null)
          Text(
            VideoSessionCopy.noChangeHint,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall,
          )
        else if (notice != null)
          Text(
            switch (notice!) {
              VideoSessionNotice.saved => VideoSessionCopy.saveSucceeded,
              VideoSessionNotice.invalidPolicy =>
                '${VideoSessionCopy.invalidPolicy}${VideoSessionCopy.invalidPolicyHint}',
              VideoSessionNotice.failed => VideoSessionCopy.settingsFailed,
            },
            key: const ValueKey<String>('video-session-notice'),
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall,
          ),
      ],
    );
  }
}

class _LoadingState extends StatelessWidget {
  const _LoadingState();

  @override
  Widget build(BuildContext context) {
    return Center(
      key: const ValueKey<String>('video-session-loading'),
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const SizedBox(
              width: 28,
              height: 28,
              child: CircularProgressIndicator(strokeWidth: 2.5),
            ),
            const SizedBox(height: 12),
            Text(
              VideoSessionCopy.loading,
              style: Theme.of(context).textTheme.bodySmall,
            ),
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
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const Icon(
              Icons.videocam_off_outlined,
              size: 28,
              color: Color(0xFFF7A8BF),
            ),
            const SizedBox(height: 10),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: onRetry,
              child: const Text(VideoSessionCopy.retryButton),
            ),
          ],
        ),
      ),
    );
  }
}
