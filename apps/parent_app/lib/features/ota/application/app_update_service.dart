import 'dart:async';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:open_filex/open_filex.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import '../../../core/error/app_exception.dart';
import '../../auth/data/auth_api.dart';
import 'resource_archive_extractor.dart';

/// Download and install progress shown by the update page.
class AppUpdateProgress {
  const AppUpdateProgress({
    required this.stage,
    required this.fraction,
    this.receivedBytes = 0,
    this.totalBytes = 0,
  });

  final AppUpdateStage stage;
  final double fraction;
  final int receivedBytes;
  final int totalBytes;
}

enum AppUpdateStage { downloading, verifying, installing, extracting }

/// Versions currently installed on this client.
///
/// Package and resource versions advance independently. Keeping both in one
/// value prevents the update page and startup gate from making different
/// decisions after a resource-only update.
class AppUpdateVersions {
  const AppUpdateVersions({
    required this.clientVersion,
    required this.resourceVersion,
  });

  final String clientVersion;
  final String resourceVersion;
}

/// Checks and applies one update through the page's cancellable flow.
abstract interface class AppUpdateCoordinator {
  /// Client and active resource versions used for update discovery.
  Future<AppUpdateVersions> installedUpdateVersions();

  /// Package version only, for diagnostics and legacy display.
  Future<String> installedVersion();

  String? updateProblem(AppUpdateInfo update);

  Future<void> installClientUpdate({
    required AppUpdateInfo update,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  });

  Future<void> installResourceUpdate({
    required AppUpdateInfo update,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  });
}

/// Installs client and resource updates after integrity verification.
///
/// Client updates are handed to the Android package installer, which keeps
/// application data in place. Resource updates are extracted into a private
/// versioned directory and become visible only after the current-version
/// marker is atomically replaced.
class AppUpdateService implements AppUpdateCoordinator {
  AppUpdateService({Dio? downloadClient})
    : _downloadClient =
          downloadClient ??
          Dio(
            BaseOptions(
              connectTimeout: const Duration(seconds: 20),
              receiveTimeout: const Duration(minutes: 10),
              sendTimeout: const Duration(seconds: 20),
            ),
          );

  static const _resourceRootName = 'resources';
  static const _currentVersionFileName = 'current_version';
  static const _clientUpdateDirectoryName = 'updates';
  static const _clientUpdateFilePrefix = 'sprout-update-';
  static const _clientUpdateRetention = Duration(hours: 24);
  final Dio _downloadClient;

  /// Reads both the installed package version and the active resource marker.
  ///
  /// A missing or unreadable marker means no resource package is active. It
  /// must not be reported as a malformed package version because first-run
  /// installations legitimately have no marker.
  @override
  Future<AppUpdateVersions> installedUpdateVersions() async {
    final packageInfo = await PackageInfo.fromPlatform();
    final supportDirectory = await getApplicationSupportDirectory();
    final marker = File(
      path.join(
        supportDirectory.path,
        _resourceRootName,
        _currentVersionFileName,
      ),
    );
    final resourceVersion = await readInstalledResourceVersion(marker);
    return AppUpdateVersions(
      clientVersion: packageInfo.version,
      resourceVersion: resourceVersion,
    );
  }

  /// Describes why an advertised update cannot be installed, if any.
  ///
  /// The platform still returns update metadata during infrastructure
  /// migrations, so callers show an actionable state instead of failing only
  /// after the guardian starts the download.
  @override
  String? updateProblem(AppUpdateInfo update) {
    // iOS delivery is not wired to an App Store/TestFlight URL yet. Do not
    // pretend an APK or arbitrary URL can install on iOS.
    if (Platform.isIOS && update.isClientUpdate) {
      return '请在 App Store 更新到新版本后继续使用';
    }
    if (update.downloadUrl.isEmpty) {
      return '更新还没有准备好，请稍后再试';
    }
    final uri = Uri.tryParse(update.downloadUrl);
    if (uri == null || uri.scheme != 'https' || uri.host.isEmpty) {
      return '更新地址不安全，请联系客服处理';
    }
    if (!RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(update.sha256)) {
      return '更新校验信息不完整，请稍后再试';
    }
    return null;
  }

  /// Returns the installed package version instead of a compile-time fallback.
  @override
  Future<String> installedVersion() async {
    final versions = await installedUpdateVersions();
    return versions.clientVersion;
  }

  /// Reads and validates the marker written after an atomic resource install.
  @visibleForTesting
  static Future<String> readInstalledResourceVersion(File marker) async {
    if (!await marker.exists()) {
      return '';
    }
    try {
      final version = (await marker.readAsString()).trim();
      return version.isEmpty ? '' : version;
    } on FileSystemException {
      return '';
    }
  }

  /// Downloads and opens the APK installer for a verified client package.
  @override
  Future<void> installClientUpdate({
    required AppUpdateInfo update,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  }) async {
    if (Platform.isIOS) {
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '请在 App Store 更新到新版本后继续使用',
        retryable: false,
      );
    }
    if (!Platform.isAndroid) {
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '当前设备不支持应用内更新，请在官方商店更新',
        retryable: false,
      );
    }
    _validateDownloadSpec(update);
    final applicationDocumentsDirectory =
        await getApplicationDocumentsDirectory();
    final updatesDirectory = Directory(
      path.join(applicationDocumentsDirectory.path, _clientUpdateDirectoryName),
    );
    final safeVersion = _safeVersionDirectory(update.version);
    final downloadedFile = File(
      path.join(
        updatesDirectory.path,
        '$_clientUpdateFilePrefix$safeVersion-'
        '${DateTime.now().microsecondsSinceEpoch}.apk',
      ),
    );

    await removeExpiredClientUpdates(
      updatesDirectory: updatesDirectory,
      currentUpdate: downloadedFile,
    );
    await _downloadAndVerify(
      update: update,
      destination: downloadedFile,
      stage: AppUpdateStage.downloading,
      onProgress: onProgress,
      cancelToken: cancelToken,
    );
    _throwIfCancelled(cancelToken);
    onProgress(
      const AppUpdateProgress(stage: AppUpdateStage.installing, fraction: 1),
    );
    final result = await OpenFilex.open(
      downloadedFile.path,
      type: 'application/vnd.android.package-archive',
    );
    switch (result.type) {
      case ResultType.done:
        break;
      case ResultType.noAppToOpen:
        throw const AppException(
          kind: AppErrorKind.serviceUnavailable,
          message: '没有打开安装页面，请稍后重试',
          retryable: true,
        );
      case ResultType.fileNotFound:
        throw const AppException(
          kind: AppErrorKind.notFound,
          message: '更新包已不存在，请重新下载更新',
          retryable: true,
        );
      case ResultType.permissionDenied:
        throw const AppException(
          kind: AppErrorKind.insufficientPermission,
          message: '请允许安装应用后重试',
          retryable: true,
        );
      case ResultType.error:
        throw const AppException(
          kind: AppErrorKind.serviceUnavailable,
          message: '安装页面没有正常打开，请稍后重试',
          retryable: true,
        );
    }
  }

  /// Downloads, verifies, and activates a resource package.
  ///
  /// A failed download, hash check, or extraction leaves the previous
  /// `current_version` marker untouched.
  @override
  Future<void> installResourceUpdate({
    required AppUpdateInfo update,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  }) async {
    _validateDownloadSpec(update);
    final supportDirectory = await getApplicationSupportDirectory();
    final resourcesRoot = Directory(
      path.join(supportDirectory.path, _resourceRootName),
    );
    final stagingDirectory = Directory(
      path.join(
        resourcesRoot.path,
        '.staging-${DateTime.now().microsecondsSinceEpoch}',
      ),
    );
    final versionDirectory = Directory(
      path.join(resourcesRoot.path, _safeVersionDirectory(update.version)),
    );
    final archiveFile = File(
      path.join(
        (await getTemporaryDirectory()).path,
        'sprout-resource-${DateTime.now().microsecondsSinceEpoch}.zip',
      ),
    );

    await resourcesRoot.create(recursive: true);
    await stagingDirectory.create(recursive: true);

    try {
      await _downloadAndVerify(
        update: update,
        destination: archiveFile,
        stage: AppUpdateStage.downloading,
        onProgress: onProgress,
        cancelToken: cancelToken,
      );
      _throwIfCancelled(cancelToken);
      onProgress(
        const AppUpdateProgress(stage: AppUpdateStage.extracting, fraction: 0),
      );
      await ResourceArchiveExtractor().extract(
        archiveFile: archiveFile,
        destination: stagingDirectory,
      );
      _throwIfCancelled(cancelToken);

      if (await versionDirectory.exists()) {
        await versionDirectory.delete(recursive: true);
      }
      await stagingDirectory.rename(versionDirectory.path);
      await _writeCurrentVersion(resourcesRoot, update.version);
      onProgress(
        const AppUpdateProgress(stage: AppUpdateStage.installing, fraction: 1),
      );
    } on Object {
      if (await stagingDirectory.exists()) {
        await stagingDirectory.delete(recursive: true);
      }
      rethrow;
    } finally {
      if (await archiveFile.exists()) {
        try {
          await archiveFile.delete();
        } on FileSystemException {
          // Cleanup failure is not part of the update result.
        }
      }
    }
  }

  Future<void> _downloadAndVerify({
    required AppUpdateInfo update,
    required File destination,
    required AppUpdateStage stage,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  }) async {
    try {
      await destination.parent.create(recursive: true);
      await _downloadClient.download(
        update.downloadUrl,
        destination.path,
        deleteOnError: true,
        cancelToken: cancelToken,
        options: Options(
          responseType: ResponseType.stream,
          headers: const {'Accept': 'application/octet-stream'},
        ),
        onReceiveProgress: (receivedBytes, totalBytes) {
          final fraction = totalBytes <= 0
              ? 0.0
              : (receivedBytes / totalBytes).clamp(0.0, 1.0);
          onProgress(
            AppUpdateProgress(
              stage: stage,
              fraction: fraction,
              receivedBytes: receivedBytes,
              totalBytes: totalBytes,
            ),
          );
        },
      );
    } on DioException catch (error) {
      throw AppException(
        kind: AppErrorKind.network,
        message: '下载没有完成，请检查网络后重试',
        retryable: true,
        cause: error,
      );
    }

    onProgress(
      const AppUpdateProgress(stage: AppUpdateStage.verifying, fraction: 1),
    );
    _throwIfCancelled(cancelToken);
    await verifySha256(destination, update.sha256);
  }

  void _throwIfCancelled(CancelToken? cancelToken) {
    final cancelError = cancelToken?.cancelError;
    if (cancelError != null) {
      throw cancelError;
    }
  }

  /// Removes client update packages that are safe to clean up.
  ///
  /// A pending Android installer may still read a recent package, so only
  /// APKs older than the retention window are removed.
  @visibleForTesting
  static Future<void> removeExpiredClientUpdates({
    required Directory updatesDirectory,
    required File currentUpdate,
    DateTime? now,
  }) async {
    if (!await updatesDirectory.exists()) {
      return;
    }

    final cutoff = (now ?? DateTime.now()).subtract(_clientUpdateRetention);
    await for (final entity in updatesDirectory.list(followLinks: false)) {
      if (entity is! File ||
          path.extension(entity.path).toLowerCase() != '.apk') {
        continue;
      }
      // The current download is reserved for the installer opened by this call.
      if (path.equals(entity.path, currentUpdate.path)) {
        continue;
      }
      if (!(await entity.lastModified()).isBefore(cutoff)) {
        continue;
      }

      try {
        await entity.delete();
      } on FileSystemException {
        // A file still held by the installer can be retried on a later update.
      }
    }
  }

  @visibleForTesting
  static Future<void> verifySha256(File file, String expectedSha256) async {
    final digest = await sha256.bind(file.openRead()).first;
    final actual = digest.toString().toLowerCase();
    if (actual != expectedSha256.toLowerCase()) {
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '更新包校验没有通过，请稍后重试',
        retryable: true,
      );
    }
  }

  Future<void> _writeCurrentVersion(
    Directory resourcesRoot,
    String version,
  ) async {
    final marker = File(path.join(resourcesRoot.path, _currentVersionFileName));
    final stagingMarker = File(
      path.join(
        resourcesRoot.path,
        '.current_version-${DateTime.now().microsecondsSinceEpoch}',
      ),
    );
    await stagingMarker.writeAsString(version, flush: true);
    try {
      await stagingMarker.rename(marker.path);
    } on FileSystemException {
      // Android does not replace an existing target on every filesystem.
      if (await marker.exists()) {
        await marker.delete();
      }
      await stagingMarker.rename(marker.path);
    }
  }

  void _validateDownloadSpec(AppUpdateInfo update) {
    final problem = updateProblem(update);
    if (problem != null) {
      throw const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '更新信息不完整，请稍后重试',
        retryable: true,
      );
    }
  }

  String _safeVersionDirectory(String version) {
    final sanitized = version.replaceAll(RegExp(r'[^0-9A-Za-z._-]'), '_');
    return sanitized.isEmpty ? 'unknown' : sanitized;
  }
}

/// Applies the installed-version guard as a compatibility layer for platforms
/// that only return a single update candidate.
///
/// A same-or-older candidate is not actionable for its own version stream.
/// Client and resource versions advance independently, so either stream can
/// still surface a newer package.
AppUpdateInfo? reconcileInstalledUpdate({
  required AppUpdateInfo? candidate,
  required AppUpdateVersions installed,
}) {
  if (candidate == null) {
    return null;
  }
  if (candidate.isClientUpdate) {
    if (installed.clientVersion.isEmpty ||
        compareAppVersions(candidate.version, installed.clientVersion) > 0) {
      return candidate;
    }
    return null;
  }
  if (!candidate.isResourceUpdate || installed.resourceVersion.isEmpty) {
    return candidate;
  }
  if (compareAppVersions(candidate.version, installed.resourceVersion) <= 0) {
    return null;
  }
  return candidate;
}

/// Loads one update using the complete installed-version context.
///
/// Sends the resource version to future platform versions and applies the
/// same-version guard locally so older servers cannot keep offering a package
/// the client already activated.
Future<AppUpdateInfo?> discoverInstalledUpdate({
  required AuthApi api,
  required String platform,
  required AppUpdateVersions installed,
}) async {
  final candidate = await api.appUpdate(
    platform: platform,
    currentVersion: installed.clientVersion,
    currentResourceVersion: installed.resourceVersion,
  );
  return reconcileInstalledUpdate(candidate: candidate, installed: installed);
}

/// Compares dotted numeric versions, treating a numeric pre-release suffix as
/// older than its release. Non-version metadata is ignored.
@visibleForTesting
int compareAppVersions(String left, String right) {
  final leftVersion = _ParsedVersion.tryParse(left);
  final rightVersion = _ParsedVersion.tryParse(right);
  if (leftVersion == null || rightVersion == null) {
    return left.compareTo(right);
  }
  final coreLength = leftVersion.core.length > rightVersion.core.length
      ? leftVersion.core.length
      : rightVersion.core.length;
  for (var index = 0; index < coreLength; index++) {
    final leftPart = index < leftVersion.core.length
        ? leftVersion.core[index]
        : 0;
    final rightPart = index < rightVersion.core.length
        ? rightVersion.core[index]
        : 0;
    final comparison = leftPart.compareTo(rightPart);
    if (comparison != 0) {
      return comparison;
    }
  }
  final leftPre = leftVersion.preRelease;
  final rightPre = rightVersion.preRelease;
  if (leftPre == null && rightPre == null) {
    return 0;
  }
  if (leftPre == null) {
    return 1;
  }
  if (rightPre == null) {
    return -1;
  }
  final preLength = leftPre.length > rightPre.length
      ? leftPre.length
      : rightPre.length;
  for (var index = 0; index < preLength; index++) {
    final leftSegment = index < leftPre.length ? leftPre[index] : '';
    final rightSegment = index < rightPre.length ? rightPre[index] : '';
    final leftNumber = int.tryParse(leftSegment);
    final rightNumber = int.tryParse(rightSegment);
    if (leftNumber != null && rightNumber != null) {
      final comparison = leftNumber.compareTo(rightNumber);
      if (comparison != 0) {
        return comparison;
      }
      continue;
    }
    if (leftNumber != null) {
      return -1;
    }
    if (rightNumber != null) {
      return 1;
    }
    final comparison = leftSegment.compareTo(rightSegment);
    if (comparison != 0) {
      return comparison;
    }
  }
  return 0;
}

class _ParsedVersion {
  const _ParsedVersion(this.core, this.preRelease);

  final List<int> core;
  final List<String>? preRelease;

  static _ParsedVersion? tryParse(String value) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      return null;
    }
    final withoutBuild = normalized.split('+').first;
    final separator = withoutBuild.indexOf('-');
    final coreText = separator == -1
        ? withoutBuild
        : withoutBuild.substring(0, separator);
    final preText = separator == -1
        ? null
        : withoutBuild.substring(separator + 1);
    final coreSegments = coreText.split('.');
    if (coreSegments.isEmpty) {
      return null;
    }
    final core = <int>[];
    for (final segment in coreSegments) {
      final number = int.tryParse(segment);
      if (number == null || number < 0) {
        return null;
      }
      core.add(number);
    }
    final preRelease = preText == null || preText.isEmpty
        ? null
        : preText.split('.');
    return _ParsedVersion(core, preRelease);
  }
}

/// Shared updater used by the software update page.
final appUpdateServiceProvider = Provider<AppUpdateService>((ref) {
  return AppUpdateService();
});
