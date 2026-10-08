import 'dart:convert';
import 'dart:io';

import 'package:open_filex/open_filex.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

/// One privacy export saved on the guardian's device.
class PrivacyExportFile {
  const PrivacyExportFile({
    required this.filePath,
    required this.exportedAt,
    required this.sizeBytes,
    this.mimeType = 'application/json',
  });

  final String filePath;
  final DateTime exportedAt;
  final int sizeBytes;
  final String mimeType;

  String get fileName => path.basename(filePath);
}

/// Opens or externally shares a privacy export file.
///
/// The production implementation is isolated here so widget tests can verify
/// the complete user flow without invoking platform channels.
abstract interface class PrivacyExportOpener {
  Future<PrivacyExportOpenResult> open(PrivacyExportFile file);

  Future<PrivacyExportShareOutcome> share(PrivacyExportFile file);
}

enum PrivacyExportOpenResult { opened, unavailable, failed }

enum PrivacyExportShareResult { shared, dismissed, unavailable, failed }

class PrivacyExportShareOutcome {
  const PrivacyExportShareOutcome(this.result);

  final PrivacyExportShareResult result;

  bool get didShare => result == PrivacyExportShareResult.shared;
  bool get wasDismissed => result == PrivacyExportShareResult.dismissed;

  bool get canRetry =>
      result == PrivacyExportShareResult.failed ||
      result == PrivacyExportShareResult.unavailable;
}

/// Production file opener backed by the operating system and share sheet.
class SystemPrivacyExportLauncher implements PrivacyExportOpener {
  const SystemPrivacyExportLauncher();

  @override
  Future<PrivacyExportOpenResult> open(PrivacyExportFile file) async {
    final result = await OpenFilex.open(file.filePath);
    switch (result.type) {
      case ResultType.done:
        return PrivacyExportOpenResult.opened;
      case ResultType.noAppToOpen:
        return PrivacyExportOpenResult.unavailable;
      case ResultType.fileNotFound:
      case ResultType.permissionDenied:
      case ResultType.error:
        return PrivacyExportOpenResult.failed;
    }
  }

  @override
  Future<PrivacyExportShareOutcome> share(PrivacyExportFile file) async {
    try {
      final result = await SharePlus.instance.share(
        ShareParams(
          files: [XFile(file.filePath, mimeType: file.mimeType)],
          fileNameOverrides: [file.fileName],
          title: '保存或分享数据副本',
          subject: '如此萌屋数据副本',
        ),
      );
      return PrivacyExportShareOutcome(switch (result.status) {
        ShareResultStatus.success => PrivacyExportShareResult.shared,
        ShareResultStatus.dismissed => PrivacyExportShareResult.dismissed,
        ShareResultStatus.unavailable => PrivacyExportShareResult.unavailable,
      });
    } on Object {
      return const PrivacyExportShareOutcome(PrivacyExportShareResult.failed);
    }
  }
}

/// Writes an export returned by the platform to durable application storage.
class PrivacyExportStore {
  PrivacyExportStore({Future<Directory> Function()? documentsDirectory})
    : _documentsDirectory =
          documentsDirectory ?? getApplicationDocumentsDirectory;

  static const _directoryName = 'privacy_exports';
  final Future<Directory> Function() _documentsDirectory;

  Future<PrivacyExportFile> save(
    Map<String, Object?> payload, {
    DateTime? now,
  }) async {
    final safePayload = redactPrivacyExport(payload);
    final exportedAt = now ?? DateTime.now();
    final root = Directory(
      path.join((await _documentsDirectory()).path, _directoryName),
    );
    await root.create(recursive: true);
    final file = File(
      path.join(root.path, 'sprout-privacy-${_timestamp(exportedAt)}.json'),
    );
    await file.writeAsString(
      const JsonEncoder.withIndent('  ').convert(safePayload),
      flush: true,
    );
    return PrivacyExportFile(
      filePath: file.path,
      exportedAt: exportedAt,
      sizeBytes: await file.length(),
    );
  }

  String _timestamp(DateTime value) {
    String twoDigits(int part) => part.toString().padLeft(2, '0');
    return '${value.year}${twoDigits(value.month)}${twoDigits(value.day)}-'
        '${twoDigits(value.hour)}${twoDigits(value.minute)}'
        '${twoDigits(value.second)}';
  }
}

/// Removes credential-shaped fields from a server-provided export.
///
/// The platform is expected to redact its response already. Repeating the
/// operation at the filesystem boundary protects the guardian even if an
/// outdated or misconfigured backend omits a field.
Map<String, Object?> redactPrivacyExport(Map<String, Object?> payload) {
  final redacted = _redactExportValue(payload);
  return Map<String, Object?>.from(redacted as Map);
}

Object? _redactExportValue(Object? value) {
  if (value is Map) {
    final result = <String, Object?>{};
    for (final entry in value.entries) {
      final key = entry.key.toString();
      if (_isSensitiveExportKey(key)) {
        continue;
      }
      result[key] = _redactExportValue(entry.value);
    }
    return result;
  }
  if (value is List) {
    return value.map(_redactExportValue).toList(growable: false);
  }
  return value;
}

bool _isSensitiveExportKey(String key) {
  final normalized = key.toLowerCase().replaceAll(RegExp(r'[^a-z0-9]'), '');
  if (normalized.isEmpty) {
    return false;
  }
  return switch (normalized) {
    'password' ||
    'passwordhash' ||
    'token' ||
    'accesstoken' ||
    'refreshtoken' ||
    'secret' ||
    'apikey' ||
    'credential' ||
    'credentials' ||
    'authorization' ||
    'cookie' ||
    'session' ||
    'sessionid' ||
    'hash' ||
    'nonce' ||
    'ciphertext' => true,
    _ =>
      normalized.contains('apikey') ||
          normalized.contains('password') ||
          normalized.endsWith('token') ||
          normalized.endsWith('secret') ||
          normalized.endsWith('credential') ||
          normalized.endsWith('nonce') ||
          normalized.endsWith('hash'),
  };
}
