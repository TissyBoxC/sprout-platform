import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/wearable_controller.dart';
import '../domain/wearable.dart';

/// Guardian view of the family wearable location, geofence, contacts and SOS.
///
/// Only bounded metadata is shown: a rounded position, the precision band and
/// the geofence result. Revoking the location permission stops sampling, upload
/// and display and clears the retained positions. A triggered SOS stays latched
/// until the guardian cancels it.
class WearablePage extends ConsumerWidget {
  const WearablePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(wearableControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('穿戴与定位'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (_) => 'wearable-data',
          error: (_, _) => 'wearable-error',
          loading: () => 'wearable-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('wearable-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('wearable-error'),
            message: WearableController.messageFor(error),
            onRetry: () =>
                ref.read(wearableControllerProvider.notifier).refresh(),
          ),
          data: (value) => _WearableBody(
            key: const ValueKey<String>('wearable-data'),
            state: value,
            onTogglePermission: (granted) =>
                _togglePermission(context, ref, value, granted),
            onSetPrecision: (precision) =>
                _setPrecision(context, ref, value, precision),
            onAddContact: () => _addContact(context, ref),
            onDeleteContact: (id) => _deleteContact(context, ref, id),
            onEditGeofence: () => _editGeofence(context, ref, value),
            onCancelSos: (id) => _cancelSos(context, ref, id),
          ),
        ),
      ),
    );
  }

  Future<void> _togglePermission(
    BuildContext context,
    WidgetRef ref,
    WearableState current,
    bool granted,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(wearableControllerProvider.notifier).updatePolicy(
            current.policy.copyWith(
              enabled: granted,
              permissionGranted: granted,
            ),
          );
      messenger.showSnackBar(
        SnackBar(content: Text(granted ? '已开启定位' : '已撤回定位授权')),
      );
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(WearableController.messageFor(error))),
      );
    }
  }

  Future<void> _setPrecision(
    BuildContext context,
    WidgetRef ref,
    WearableState current,
    String precision,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(wearableControllerProvider.notifier).updatePolicy(
            current.policy.copyWith(precision: precision),
          );
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(WearableController.messageFor(error))),
      );
    }
  }

  Future<void> _addContact(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.of(context);
    final draft = await showDialog<_ContactDraft>(
      context: context,
      builder: (dialogContext) => const _ContactDialog(),
    );
    if (draft == null) {
      return;
    }
    try {
      await ref.read(wearableControllerProvider.notifier).createContact(
            EmergencyContact(
              id: '',
              childId: '',
              displayName: draft.displayName,
              phone: draft.phone,
              role: draft.role,
              priority: 0,
              enabled: true,
            ),
          );
      messenger.showSnackBar(const SnackBar(content: Text('紧急联系人已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(WearableController.messageFor(error))),
      );
    }
  }

  Future<void> _deleteContact(
    BuildContext context,
    WidgetRef ref,
    String contactId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(wearableControllerProvider.notifier)
          .deleteContact(contactId);
      messenger.showSnackBar(const SnackBar(content: Text('紧急联系人已删除')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(WearableController.messageFor(error))),
      );
    }
  }

  Future<void> _editGeofence(
    BuildContext context,
    WidgetRef ref,
    WearableState current,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    final draft = await showDialog<_GeofenceDraft>(
      context: context,
      builder: (dialogContext) => _GeofenceDialog(existing: current.geofence),
    );
    if (draft == null) {
      return;
    }
    try {
      await ref.read(wearableControllerProvider.notifier).updateGeofence(
            HomeGeofence(
              id: current.geofence?.id ?? '',
              childId: current.geofence?.childId ?? '',
              latitude: draft.latitude,
              longitude: draft.longitude,
              radiusMeters: draft.radiusMeters,
              enabled: true,
            ),
          );
      messenger.showSnackBar(const SnackBar(content: Text('电子围栏已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(WearableController.messageFor(error))),
      );
    }
  }

  Future<void> _cancelSos(
    BuildContext context,
    WidgetRef ref,
    String eventId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(wearableControllerProvider.notifier).cancelSos(eventId);
      messenger.showSnackBar(const SnackBar(content: Text('SOS 已解除')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(WearableController.messageFor(error))),
      );
    }
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

class _WearableBody extends StatelessWidget {
  const _WearableBody({
    required this.state,
    required this.onTogglePermission,
    required this.onSetPrecision,
    required this.onAddContact,
    required this.onDeleteContact,
    required this.onEditGeofence,
    required this.onCancelSos,
    super.key,
  });

  final WearableState state;
  final ValueChanged<bool> onTogglePermission;
  final ValueChanged<String> onSetPrecision;
  final VoidCallback onAddContact;
  final ValueChanged<String> onDeleteContact;
  final VoidCallback onEditGeofence;
  final ValueChanged<String> onCancelSos;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        if (state.hasActiveSos)
          AppReveal(
            child: Card(
              color: Theme.of(context).colorScheme.errorContainer,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'SOS 紧急求助生效中',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    for (final event in state.activeSos)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.sos_rounded),
                        title: Text(event.deviceId.isEmpty ? '设备' : event.deviceId),
                        subtitle: Text(_coordinateLabel(event.latitude, event.longitude)),
                        trailing: FilledButton(
                          onPressed: () => onCancelSos(event.id),
                          child: const Text('解除'),
                        ),
                      ),
                  ],
                ),
              ),
            ),
          ),
        if (state.hasActiveSos) const SizedBox(height: 16),
        AppReveal(
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('定位权限', style: Theme.of(context).textTheme.titleMedium),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('允许采集与上传位置'),
                    subtitle: const Text('撤回授权将停止采集、上传和展示，并清理已保存的位置'),
                    value: state.policy.permissionGranted,
                    onChanged: onTogglePermission,
                  ),
                  const SizedBox(height: 8),
                  Text('定位精度', style: Theme.of(context).textTheme.labelLarge),
                  const SizedBox(height: 8),
                  SegmentedButton<String>(
                    segments: const [
                      ButtonSegment(value: 'coarse', label: Text('粗略')),
                      ButtonSegment(value: 'balanced', label: Text('平衡')),
                      ButtonSegment(value: 'precise', label: Text('精确')),
                    ],
                    selected: <String>{state.policy.precision},
                    onSelectionChanged: (selection) =>
                        onSetPrecision(selection.first),
                  ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(
                        '家庭电子围栏',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      TextButton.icon(
                        onPressed: onEditGeofence,
                        icon: const Icon(Icons.edit_location_alt_outlined),
                        label: const Text('设置'),
                      ),
                    ],
                  ),
                  if (state.geofence == null)
                    const Text('还没有设置家庭地址。')
                  else
                    Text(
                      '中心 ${_coordinateLabel(state.geofence!.latitude, state.geofence!.longitude)}'
                      ' · 半径 ${state.geofence!.radiusMeters} 米',
                    ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(
                        '紧急联系人',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      IconButton(
                        tooltip: '添加联系人',
                        onPressed: onAddContact,
                        icon: const Icon(Icons.person_add_alt_rounded),
                      ),
                    ],
                  ),
                  if (state.contacts.isEmpty)
                    const Text('还没有紧急联系人。')
                  else
                    for (final contact in state.contacts)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.contact_phone_outlined),
                        title: Text(contact.displayName),
                        subtitle: Text('${_roleLabel(contact.role)} · ${contact.phone}'),
                        trailing: IconButton(
                          tooltip: '删除联系人',
                          onPressed: () => onDeleteContact(contact.id),
                          icon: const Icon(Icons.delete_outline_rounded),
                        ),
                      ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('设备定位', style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 8),
                  if (state.devices.isEmpty)
                    const Text('当前没有上报定位的设备。')
                  else
                    for (final device in state.devices)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(_deviceIcon(device.geofenceState)),
                        title: Text(device.deviceId),
                        subtitle: Text(
                          '${_geofenceLabel(device.geofenceState)} · ${_precisionLabel(device.precision)}',
                        ),
                      ),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class _ContactDraft {
  const _ContactDraft({
    required this.displayName,
    required this.phone,
    required this.role,
  });

  final String displayName;
  final String phone;
  final String role;
}

class _ContactDialog extends StatefulWidget {
  const _ContactDialog();

  @override
  State<_ContactDialog> createState() => _ContactDialogState();
}

class _ContactDialogState extends State<_ContactDialog> {
  final TextEditingController _name = TextEditingController();
  final TextEditingController _phone = TextEditingController();
  String _role = 'guardian';

  @override
  void dispose() {
    _name.dispose();
    _phone.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('添加紧急联系人'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: _name,
            decoration: const InputDecoration(labelText: '名称'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _phone,
            keyboardType: TextInputType.phone,
            decoration: const InputDecoration(labelText: '电话号码'),
          ),
          const SizedBox(height: 12),
          DropdownButtonFormField<String>(
            initialValue: _role,
            decoration: const InputDecoration(labelText: '角色'),
            items: const [
              DropdownMenuItem(value: 'guardian', child: Text('监护人')),
              DropdownMenuItem(value: 'family', child: Text('家人')),
              DropdownMenuItem(value: 'service', child: Text('服务')),
            ],
            onChanged: (value) => setState(() => _role = value ?? 'guardian'),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(
            _ContactDraft(
              displayName: _name.text.trim(),
              phone: _phone.text.trim(),
              role: _role,
            ),
          ),
          child: const Text('保存'),
        ),
      ],
    );
  }
}

class _GeofenceDraft {
  const _GeofenceDraft({
    required this.latitude,
    required this.longitude,
    required this.radiusMeters,
  });

  final double latitude;
  final double longitude;
  final int radiusMeters;
}

class _GeofenceDialog extends StatefulWidget {
  const _GeofenceDialog({this.existing});

  final HomeGeofence? existing;

  @override
  State<_GeofenceDialog> createState() => _GeofenceDialogState();
}

class _GeofenceDialogState extends State<_GeofenceDialog> {
  late final TextEditingController _latitude;
  late final TextEditingController _longitude;
  late final TextEditingController _radius;

  @override
  void initState() {
    super.initState();
    _latitude = TextEditingController(
      text: widget.existing?.latitude.toString() ?? '',
    );
    _longitude = TextEditingController(
      text: widget.existing?.longitude.toString() ?? '',
    );
    _radius = TextEditingController(
      text: (widget.existing?.radiusMeters ?? 500).toString(),
    );
  }

  @override
  void dispose() {
    _latitude.dispose();
    _longitude.dispose();
    _radius.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('设置家庭电子围栏'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: _latitude,
            keyboardType: const TextInputType.numberWithOptions(
              decimal: true,
              signed: true,
            ),
            decoration: const InputDecoration(labelText: '纬度'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _longitude,
            keyboardType: const TextInputType.numberWithOptions(
              decimal: true,
              signed: true,
            ),
            decoration: const InputDecoration(labelText: '经度'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _radius,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(labelText: '半径（米）'),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(
            _GeofenceDraft(
              latitude: double.tryParse(_latitude.text.trim()) ?? 0,
              longitude: double.tryParse(_longitude.text.trim()) ?? 0,
              radiusMeters: int.tryParse(_radius.text.trim()) ?? 500,
            ),
          ),
          child: const Text('保存'),
        ),
      ],
    );
  }
}

String _coordinateLabel(double latitude, double longitude) {
  return '${latitude.toStringAsFixed(3)}, ${longitude.toStringAsFixed(3)}';
}

String _roleLabel(String role) {
  return (
    {
      'guardian': '监护人',
      'family': '家人',
      'service': '服务',
    }[role] ?? role);
}

String _precisionLabel(String precision) {
  return (
    {
      'coarse': '粗略定位',
      'balanced': '平衡定位',
      'precise': '精确定位',
    }[precision] ?? precision);
}

String _geofenceLabel(String state) {
  return (
    {
      'inside': '在围栏内',
      'outside': '越界',
      'unknown': '围栏未知',
    }[state] ?? state);
}

IconData _deviceIcon(String geofenceState) {
  switch (geofenceState) {
    case 'outside':
      return Icons.wrong_location_outlined;
    case 'inside':
      return Icons.my_location_rounded;
    default:
      return Icons.location_searching_rounded;
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry, super.key});

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
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重试')),
          ],
        ),
      ),
    );
  }
}
