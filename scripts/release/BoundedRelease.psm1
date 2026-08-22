Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ConfirmPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
$env:GIT_TERMINAL_PROMPT = '0'

$script:V013ReleaseFiles = @(
    'teamkit-v0.1.3-windows-amd64.exe', 'teamkit-v0.1.3-linux-amd64',
    'teamkit-v0.1.3-darwin-amd64', 'teamkit-v0.1.3-darwin-arm64',
    'SHA256SUMS', 'SECURITY-AUDIT.json'
)
$script:V015ReleaseFiles = @(
    'teamkit-v0.1.5-windows-amd64.exe', 'teamkit-v0.1.5-linux-amd64',
    'teamkit-v0.1.5-darwin-amd64', 'teamkit-v0.1.5-darwin-arm64',
    'SHA256SUMS', 'SECURITY-AUDIT.json'
)
$script:UploadFiles = @(
    @{ Name = 'Hermes-Setup.exe'; Url = 'https://gitlab.example.invalid/-/project/12087/uploads/0f99502ae0755ee2648473811338b66f/Hermes-Setup.exe'; Size = 7597376; Sha256 = '505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5' },
    @{ Name = 'certs.zip'; Url = 'https://gitlab.example.invalid/-/project/12087/uploads/d775983a3143a0556c0d4665896e1b38/certs.zip'; Size = 136410; Sha256 = '88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8' }
)

if (-not ('TeamKit.Release.BoundedCapture' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace TeamKit.Release {
    public sealed class CaptureResult {
        public string Text { get; set; }
        public bool Overflowed { get; set; }
    }

    public static class BoundedCapture {
        public static Task<CaptureResult> DrainAsync(StreamReader reader, int maxBytes, TaskCompletionSource<bool> overflowSignal) {
            return DrainAsync(reader, maxBytes, overflowSignal, CancellationToken.None);
        }

        public static async Task<CaptureResult> DrainAsync(StreamReader reader, int maxBytes, TaskCompletionSource<bool> overflowSignal, CancellationToken cancellationToken) {
            var buffer = new char[8192];
            var text = new StringBuilder();
            var bytes = 0;
            var overflowed = false;
            int read;
            while ((read = await reader.ReadAsync(buffer.AsMemory(0, buffer.Length), cancellationToken).ConfigureAwait(false)) != 0) {
                var chunkBytes = Encoding.UTF8.GetByteCount(buffer, 0, read);
                if (bytes + chunkBytes <= maxBytes) {
                    text.Append(buffer, 0, read);
                    bytes += chunkBytes;
                } else {
                    overflowed = true;
                    overflowSignal.TrySetResult(true);
                }
                // Continue draining after the bound. This prevents a child from
                // blocking on a full OS pipe while keeping retained output bounded.
            }
            return new CaptureResult { Text = text.ToString(), Overflowed = overflowed };
        }

        public static Task<byte[]> ReadBoundedAsync(Stream stream, int maxBytes) {
            return ReadBoundedAsync(stream, maxBytes, CancellationToken.None);
        }

        public static async Task<byte[]> ReadBoundedAsync(Stream stream, int maxBytes, CancellationToken cancellationToken) {
            var buffer = new byte[81920];
            using (var output = new MemoryStream()) {
                int read;
                while ((read = await stream.ReadAsync(buffer, 0, buffer.Length, cancellationToken).ConfigureAwait(false)) != 0) {
                    if (output.Length + read > maxBytes) throw new InvalidDataException("response body exceeds bound");
                    await output.WriteAsync(buffer, 0, read, cancellationToken).ConfigureAwait(false);
                }
                return output.ToArray();
            }
        }
    }
}
'@
}

# A Process.Kill($true) call is not sufficient once a direct child has exited:
# Windows can reparent a descendant which still owns an inherited stdout/stderr
# pipe.  The launcher therefore creates each production child suspended and
# assigns it to a kill-on-close Job Object before its first instruction runs.
if (-not ('TeamKit.Release.WindowsJobProcess' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Collections;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;

namespace TeamKit.Release {
    public sealed class WindowsJobProcess : IDisposable {
        private const uint JobObjectExtendedLimitInformation = 9;
        private const uint JobObjectLimitKillOnJobClose = 0x00002000;
        private const uint HandleFlagInherit = 0x00000001;
        private const uint StartfUseStdHandles = 0x00000100;
        private const uint ProcThreadAttributeHandleList = 0x00020002;
        private const uint CreateSuspended = 0x00000004;
        private const uint CreateUnicodeEnvironment = 0x00000400;
        private const uint ExtendedStartupInfoPresent = 0x00080000;
        private const uint CreateNoWindow = 0x08000000;
        private const uint GenericRead = 0x80000000;
        private const uint FileShareRead = 0x00000001;
        private const uint FileShareWrite = 0x00000002;
        private const uint OpenExisting = 3;
        private static readonly IntPtr InvalidHandleValue = new IntPtr(-1);

        private readonly object gate = new object();
        private IntPtr job;
        private bool disposed;

        public Process Process { get; private set; }
        public StreamReader StandardOutput { get; private set; }
        public StreamReader StandardError { get; private set; }
        // A deny-only seam lets the local regression prove that a suspended
        // process is never resumed if Job assignment cannot be established.
        public static bool ForceAssignmentFailureForTests { get; set; }
        public static bool IsSupported {
            get { return RuntimeInformation.IsOSPlatform(OSPlatform.Windows); }
        }

        private WindowsJobProcess(IntPtr job, Process process, StreamReader standardOutput, StreamReader standardError) {
            this.job = job;
            this.Process = process;
            this.StandardOutput = standardOutput;
            this.StandardError = standardError;
        }

        public static WindowsJobProcess Start(string fileName, string[] arguments) {
            if (!IsSupported) throw new PlatformNotSupportedException("bounded publisher requires Windows process containment");
            if (String.IsNullOrWhiteSpace(fileName) || fileName.IndexOf('\0') >= 0) throw new InvalidOperationException("invalid bounded process executable");
            return StartContained(fileName, arguments ?? new string[0]);
        }

        public void TerminateTree() {
            lock (gate) {
                if (job == IntPtr.Zero) return;
                try { TerminateJobObject(job, 1); } catch { }
                try { CloseHandle(job); } catch { }
                job = IntPtr.Zero;
            }
        }

        public void Dispose() {
            if (disposed) return;
            disposed = true;
            TerminateTree();
            try { if (StandardOutput != null) StandardOutput.Dispose(); } catch { }
            try { if (StandardError != null) StandardError.Dispose(); } catch { }
            try { if (Process != null) Process.Dispose(); } catch { }
        }

        private static WindowsJobProcess StartContained(string fileName, string[] arguments) {
            IntPtr job = IntPtr.Zero;
            IntPtr stdoutRead = IntPtr.Zero, stdoutWrite = IntPtr.Zero;
            IntPtr stderrRead = IntPtr.Zero, stderrWrite = IntPtr.Zero;
            IntPtr stdin = IntPtr.Zero;
            IntPtr attributeList = IntPtr.Zero, handleList = IntPtr.Zero, environment = IntPtr.Zero;
            PROCESS_INFORMATION processInformation = new PROCESS_INFORMATION();
            Process attachedProcess = null;
            StreamReader stdout = null, stderr = null;
            try {
                job = CreateJobObjectW(IntPtr.Zero, null);
                if (job == IntPtr.Zero || job == InvalidHandleValue || !SetHandleInformation(job, HandleFlagInherit, 0)) throw ContainmentFailure();
                JOBOBJECT_EXTENDED_LIMIT_INFORMATION limits = new JOBOBJECT_EXTENDED_LIMIT_INFORMATION();
                limits.BasicLimitInformation.LimitFlags = JobObjectLimitKillOnJobClose;
                if (!SetInformationJobObject(job, JobObjectExtendedLimitInformation, ref limits, (uint)Marshal.SizeOf(typeof(JOBOBJECT_EXTENDED_LIMIT_INFORMATION)))) throw ContainmentFailure();

                SECURITY_ATTRIBUTES inheritable = new SECURITY_ATTRIBUTES();
                inheritable.nLength = Marshal.SizeOf(typeof(SECURITY_ATTRIBUTES));
                inheritable.bInheritHandle = 1;
                if (!CreatePipe(out stdoutRead, out stdoutWrite, ref inheritable, 0) || !SetHandleInformation(stdoutRead, HandleFlagInherit, 0)) throw ContainmentFailure();
                if (!CreatePipe(out stderrRead, out stderrWrite, ref inheritable, 0) || !SetHandleInformation(stderrRead, HandleFlagInherit, 0)) throw ContainmentFailure();
                stdin = CreateFileW("NUL", GenericRead, FileShareRead | FileShareWrite, ref inheritable, OpenExisting, 0, IntPtr.Zero);
                if (stdin == IntPtr.Zero || stdin == InvalidHandleValue) throw ContainmentFailure();

                IntPtr attributeSize = IntPtr.Zero;
                InitializeProcThreadAttributeList(IntPtr.Zero, 1, 0, ref attributeSize);
                if (attributeSize == IntPtr.Zero) throw ContainmentFailure();
                attributeList = Marshal.AllocHGlobal(attributeSize);
                if (!InitializeProcThreadAttributeList(attributeList, 1, 0, ref attributeSize)) throw ContainmentFailure();
                handleList = Marshal.AllocHGlobal(IntPtr.Size * 3);
                Marshal.WriteIntPtr(handleList, 0 * IntPtr.Size, stdin);
                Marshal.WriteIntPtr(handleList, 1 * IntPtr.Size, stdoutWrite);
                Marshal.WriteIntPtr(handleList, 2 * IntPtr.Size, stderrWrite);
                if (!UpdateProcThreadAttribute(attributeList, 0, (IntPtr)ProcThreadAttributeHandleList, handleList, (IntPtr)(IntPtr.Size * 3), IntPtr.Zero, IntPtr.Zero)) throw ContainmentFailure();

                STARTUPINFOEX startup = new STARTUPINFOEX();
                startup.StartupInfo.cb = Marshal.SizeOf(typeof(STARTUPINFOEX));
                startup.StartupInfo.dwFlags = StartfUseStdHandles;
                startup.StartupInfo.hStdInput = stdin;
                startup.StartupInfo.hStdOutput = stdoutWrite;
                startup.StartupInfo.hStdError = stderrWrite;
                startup.lpAttributeList = attributeList;
                environment = BuildEnvironmentBlock();
                StringBuilder commandLine = new StringBuilder(BuildCommandLine(fileName, arguments));
                uint flags = CreateSuspended | CreateUnicodeEnvironment | ExtendedStartupInfoPresent | CreateNoWindow;
                if (!CreateProcessW(null, commandLine, IntPtr.Zero, IntPtr.Zero, true, flags, environment, null, ref startup, out processInformation)) throw ContainmentFailure();

                CloseAndZero(ref stdin);
                CloseAndZero(ref stdoutWrite);
                CloseAndZero(ref stderrWrite);
                if (ForceAssignmentFailureForTests || !AssignProcessToJobObject(job, processInformation.hProcess)) throw ContainmentFailure();
                attachedProcess = Process.GetProcessById((int)processInformation.dwProcessId);
                IntPtr attachedHandle = attachedProcess.Handle;
                if (attachedHandle == IntPtr.Zero || attachedHandle == InvalidHandleValue) throw ContainmentFailure();
                if (ResumeThread(processInformation.hThread) == UInt32.MaxValue) throw ContainmentFailure();
                CloseAndZero(ref processInformation.hThread);
                CloseAndZero(ref processInformation.hProcess);

                stdout = CreateReader(ref stdoutRead);
                stderr = CreateReader(ref stderrRead);
                WindowsJobProcess result = new WindowsJobProcess(job, attachedProcess, stdout, stderr);
                job = IntPtr.Zero;
                attachedProcess = null;
                stdout = null;
                stderr = null;
                return result;
            } catch {
                try { if (processInformation.hProcess != IntPtr.Zero) TerminateProcess(processInformation.hProcess, 1); } catch { }
                try { if (job != IntPtr.Zero) TerminateJobObject(job, 1); } catch { }
                throw;
            } finally {
                try { if (attributeList != IntPtr.Zero) DeleteProcThreadAttributeList(attributeList); } catch { }
                if (attributeList != IntPtr.Zero) Marshal.FreeHGlobal(attributeList);
                if (handleList != IntPtr.Zero) Marshal.FreeHGlobal(handleList);
                if (environment != IntPtr.Zero) Marshal.FreeHGlobal(environment);
                CloseAndZero(ref stdin);
                CloseAndZero(ref stdoutWrite);
                CloseAndZero(ref stderrWrite);
                CloseAndZero(ref processInformation.hThread);
                CloseAndZero(ref processInformation.hProcess);
                CloseAndZero(ref stdoutRead);
                CloseAndZero(ref stderrRead);
                if (job != IntPtr.Zero) CloseAndZero(ref job);
                try { if (stdout != null) stdout.Dispose(); } catch { }
                try { if (stderr != null) stderr.Dispose(); } catch { }
                try { if (attachedProcess != null) attachedProcess.Dispose(); } catch { }
            }
        }

        private static StreamReader CreateReader(ref IntPtr handle) {
            IntPtr owned = handle;
            handle = IntPtr.Zero;
            FileStream stream = new FileStream(new SafeFileHandle(owned, true), FileAccess.Read, 4096, false);
            return new StreamReader(stream, new UTF8Encoding(false), true, 4096, false);
        }

        private static void CloseAndZero(ref IntPtr handle) {
            if (handle == IntPtr.Zero || handle == InvalidHandleValue) { handle = IntPtr.Zero; return; }
            CloseHandle(handle);
            handle = IntPtr.Zero;
        }

        private static Exception ContainmentFailure() {
            return new InvalidOperationException("bounded process containment unavailable");
        }

        private static IntPtr BuildEnvironmentBlock() {
            SortedDictionary<string, string> values = new SortedDictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            foreach (DictionaryEntry entry in Environment.GetEnvironmentVariables()) {
                string name = entry.Key == null ? String.Empty : entry.Key.ToString();
                if (String.IsNullOrEmpty(name) || name.Equals("GH_TOKEN", StringComparison.OrdinalIgnoreCase) || name.Equals("GITLAB_TOKEN", StringComparison.OrdinalIgnoreCase)) continue;
                values[name] = entry.Value == null ? String.Empty : entry.Value.ToString();
            }
            StringBuilder block = new StringBuilder();
            foreach (KeyValuePair<string, string> item in values) {
                if (item.Key.IndexOf('\0') >= 0 || item.Value.IndexOf('\0') >= 0) throw new InvalidOperationException("invalid bounded process environment");
                block.Append(item.Key).Append('=').Append(item.Value).Append('\0');
            }
            block.Append('\0');
            return Marshal.StringToHGlobalUni(block.ToString());
        }

        private static string BuildCommandLine(string fileName, string[] arguments) {
            StringBuilder line = new StringBuilder();
            AppendQuotedArgument(line, fileName);
            foreach (string argument in arguments) {
                line.Append(' ');
                AppendQuotedArgument(line, argument ?? String.Empty);
            }
            return line.ToString();
        }

        private static void AppendQuotedArgument(StringBuilder output, string value) {
            if (value.IndexOf('\0') >= 0) throw new InvalidOperationException("invalid bounded process argument");
            output.Append('"');
            int slashes = 0;
            foreach (char character in value) {
                if (character == '\\') { slashes++; continue; }
                if (character == '"') {
                    output.Append('\\', slashes * 2 + 1);
                    output.Append('"');
                    slashes = 0;
                    continue;
                }
                if (slashes > 0) output.Append('\\', slashes);
                slashes = 0;
                output.Append(character);
            }
            if (slashes > 0) output.Append('\\', slashes * 2);
            output.Append('"');
        }

        [StructLayout(LayoutKind.Sequential)]
        private struct SECURITY_ATTRIBUTES { public int nLength; public IntPtr lpSecurityDescriptor; public int bInheritHandle; }
        [StructLayout(LayoutKind.Sequential)]
        private struct STARTUPINFO {
            public int cb; public IntPtr lpReserved; public IntPtr lpDesktop; public IntPtr lpTitle;
            public int dwX; public int dwY; public int dwXSize; public int dwYSize; public int dwXCountChars; public int dwYCountChars;
            public int dwFillAttribute; public uint dwFlags; public short wShowWindow; public short cbReserved2; public IntPtr lpReserved2;
            public IntPtr hStdInput; public IntPtr hStdOutput; public IntPtr hStdError;
        }
        [StructLayout(LayoutKind.Sequential)]
        private struct STARTUPINFOEX { public STARTUPINFO StartupInfo; public IntPtr lpAttributeList; }
        [StructLayout(LayoutKind.Sequential)]
        private struct PROCESS_INFORMATION { public IntPtr hProcess; public IntPtr hThread; public uint dwProcessId; public uint dwThreadId; }
        [StructLayout(LayoutKind.Sequential)]
        private struct JOBOBJECT_BASIC_LIMIT_INFORMATION {
            public long PerProcessUserTimeLimit; public long PerJobUserTimeLimit; public uint LimitFlags;
            public UIntPtr MinimumWorkingSetSize; public UIntPtr MaximumWorkingSetSize; public uint ActiveProcessLimit;
            public UIntPtr Affinity; public uint PriorityClass; public uint SchedulingClass;
        }
        [StructLayout(LayoutKind.Sequential)]
        private struct IO_COUNTERS { public ulong ReadOperationCount; public ulong WriteOperationCount; public ulong OtherOperationCount; public ulong ReadTransferCount; public ulong WriteTransferCount; public ulong OtherTransferCount; }
        [StructLayout(LayoutKind.Sequential)]
        private struct JOBOBJECT_EXTENDED_LIMIT_INFORMATION {
            public JOBOBJECT_BASIC_LIMIT_INFORMATION BasicLimitInformation; public IO_COUNTERS IoInfo;
            public UIntPtr ProcessMemoryLimit; public UIntPtr JobMemoryLimit; public UIntPtr PeakProcessMemoryUsed; public UIntPtr PeakJobMemoryUsed;
        }

        [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern IntPtr CreateJobObjectW(IntPtr attributes, string name);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool SetInformationJobObject(IntPtr job, uint infoClass, ref JOBOBJECT_EXTENDED_LIMIT_INFORMATION info, uint infoLength);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool TerminateJobObject(IntPtr job, uint exitCode);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool CloseHandle(IntPtr handle);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool SetHandleInformation(IntPtr handle, uint mask, uint flags);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool CreatePipe(out IntPtr readPipe, out IntPtr writePipe, ref SECURITY_ATTRIBUTES attributes, uint size);
        [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern IntPtr CreateFileW(string name, uint desiredAccess, uint shareMode, ref SECURITY_ATTRIBUTES attributes, uint creationDisposition, uint flags, IntPtr template);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool InitializeProcThreadAttributeList(IntPtr list, int count, int flags, ref IntPtr size);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool UpdateProcThreadAttribute(IntPtr list, uint flags, IntPtr attribute, IntPtr value, IntPtr size, IntPtr previousValue, IntPtr returnSize);
        [DllImport("kernel32.dll")]
        private static extern void DeleteProcThreadAttributeList(IntPtr list);
        [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern bool CreateProcessW(string applicationName, StringBuilder commandLine, IntPtr processAttributes, IntPtr threadAttributes, bool inheritHandles, uint creationFlags, IntPtr environment, string currentDirectory, ref STARTUPINFOEX startupInfo, out PROCESS_INFORMATION processInformation);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern uint ResumeThread(IntPtr thread);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool TerminateProcess(IntPtr process, uint exitCode);
    }
}
'@
}

$script:MaxCapturedProcessBytes = 262144
$script:MaxPublicationArchiveCompressedBytes = 536870912
$script:MaxPublicationArchiveUncompressedBytes = 1073741824
$script:GenericPackageStatuses = @('default', 'hidden', 'processing', 'error', 'pending_destruction', 'deprecated')
$script:MaxPackageInventoryPages = 100

function New-ReleaseContext {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$CandidateSha,
        [ValidateSet('v0.1.3', 'v0.1.5')][string]$Version = 'v0.1.3',
        [ValidateRange(1, 180)][int]$MaxMinutes = 180,
        [string]$GitHubToken = $env:GH_TOKEN,
        [string]$GitLabToken = $env:GITLAB_TOKEN,
        [string]$GitLabBaseUrl = $env:TEAMKIT_RELEASE_GITLAB_BASE_URL,
        [scriptblock]$ProcessAdapter,
        [scriptblock]$HttpAdapter,
        [scriptblock]$SleepAdapter,
        [scriptblock]$ClockAdapter,
        [System.Diagnostics.Stopwatch]$Stopwatch,
        [scriptblock]$UtcNowAdapter,
        [scriptblock]$PairedCIAdapter,
        [scriptblock]$OrderAdapter,
        [long]$GitHubRunId,
        [long]$GitHubArtifactId,
        [long]$GitLabPipelineId,
        [long]$GitLabHandoffJobId,
        [long]$GitLabVerifyJobId,
        [hashtable[]]$UploadFiles = $script:UploadFiles
    )
    if ([string]::IsNullOrWhiteSpace($GitLabBaseUrl)) { $GitLabBaseUrl = $env:CI_SERVER_URL }
    if ([string]::IsNullOrWhiteSpace($GitLabBaseUrl)) { $GitLabBaseUrl = 'https://gitlab.example.invalid' }
    $GitLabBaseUrl = $GitLabBaseUrl.TrimEnd('/')
    $packageFirst = $Version -eq 'v0.1.5'
    if ($packageFirst -and $GitLabHandoffJobId -le 0) { $GitLabHandoffJobId = $GitLabVerifyJobId }
    if ($packageFirst) {
        foreach ($value in @($GitHubRunId, $GitHubArtifactId, $GitLabPipelineId, $GitLabHandoffJobId, $GitLabVerifyJobId)) {
            if ($value -le 0) { throw 'v0.1.5 requires exact verified CI run, artifact, pipeline and job IDs' }
        }
    }
    $releaseFiles = if ($packageFirst) { @($script:V015ReleaseFiles) } else { @($script:V013ReleaseFiles) }
    $githubRepository = if ($packageFirst) { 'i437918/kit-all-team' } else { 'mi1man-cmd/kit-all-team' }
    [hashtable[]]$selectedUploads = if ($packageFirst) { @() } else { @($UploadFiles) }
    $watch = if ($Stopwatch) { $Stopwatch } else { [System.Diagnostics.Stopwatch]::StartNew() }
    [pscustomobject]@{
        CandidateSha = $CandidateSha.ToLowerInvariant(); Version = $Version; DeadlineSeconds = $MaxMinutes * 60
        Stopwatch = $watch; GitHubToken = $GitHubToken; GitLabToken = $GitLabToken
        GitHubRepository = $githubRepository; GitLabBaseUrl = $GitLabBaseUrl
        GitLabProjectId = 12087; GitHubBranch = 'main'; GitLabBranch = 'master'
        PackageFirst = $packageFirst; PackageName = if ($packageFirst) { 'teamkit' } else { $null }; PackageVersion = if ($packageFirst) { $Version } else { $null }
        ReleaseFiles = $releaseFiles
        VerifiedCI = if ($packageFirst) { @{ github_run_id = $GitHubRunId; github_artifact_id = $GitHubArtifactId; gitlab_pipeline_id = $GitLabPipelineId; gitlab_handoff_job_id = $GitLabHandoffJobId; gitlab_job_id = $GitLabVerifyJobId } } else { $null }
        Events = [System.Collections.Generic.List[string]]::new()
        ProcessAdapter = $ProcessAdapter; HttpAdapter = $HttpAdapter
        SleepAdapter = $SleepAdapter; ClockAdapter = $ClockAdapter; UtcNowAdapter = $UtcNowAdapter; PairedCIAdapter = $PairedCIAdapter; OrderAdapter = $OrderAdapter
        UploadFiles = $selectedUploads
        State = @{ CurrentStage = 'preflight'; FailureReasonCode = $null; ForwardOnly = $false }
    }
}

function Get-RemainingSeconds {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    return [int][Math]::Floor((Get-RemainingBudgetSeconds $Context))
}

function Get-RemainingBudgetSeconds {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    $elapsed = if ($Context.ClockAdapter) { [double](& $Context.ClockAdapter $Context) } else { $Context.Stopwatch.Elapsed.TotalSeconds }
    return [Math]::Max([double]0, [double]$Context.DeadlineSeconds - $elapsed)
}

function Get-ElapsedSeconds {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    return [int][Math]::Floor([Math]::Min([double]$Context.DeadlineSeconds, [double]$Context.DeadlineSeconds - (Get-RemainingBudgetSeconds $Context)))
}

function Get-ReleaseUtcNow {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    $now = if ($Context.UtcNowAdapter) { [datetime](& $Context.UtcNowAdapter $Context) } else { [datetime]::UtcNow }
    $utc = $now.ToUniversalTime()
    return $utc.AddTicks(-($utc.Ticks % [TimeSpan]::TicksPerSecond))
}

function ConvertTo-ReleaseUtcTimestamp {
    [CmdletBinding()]
    param($Value)
    if ([string]::IsNullOrWhiteSpace([string]$Value)) { return $null }
    try { return ([datetime]::Parse([string]$Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal)).ToUniversalTime() } catch { return $null }
}

function Get-ReleaseObjectValue {
    [CmdletBinding()]
    param($Object, [Parameter(Mandatory)][string]$Name)
    if ($null -eq $Object) { return $null }
    if ($Object -is [hashtable]) { return $Object[$Name] }
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property) { return $null }
    return $property.Value
}

function Get-EffectiveGitLabProjectAccessLevel {
    [CmdletBinding()]
    param($Project)
    $permissions = Get-ReleaseObjectValue $Project 'permissions'
    $effectiveLevel = 0
    foreach ($scopeName in @('project_access', 'group_access')) {
        $scope = Get-ReleaseObjectValue $permissions $scopeName
        $accessLevel = Get-ReleaseObjectValue $scope 'access_level'
        if ($null -ne $accessLevel) { $effectiveLevel = [Math]::Max($effectiveLevel, [int]$accessLevel) }
    }
    return $effectiveLevel
}

function Assert-RemainingBudget {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, [int]$MinimumSeconds = 1)
    $remaining = Get-RemainingBudgetSeconds $Context
    if ($remaining -lt $MinimumSeconds) {
        if ($remaining -le 0) { throw [System.TimeoutException]::new("release deadline budget exhausted") }
        throw [System.InvalidOperationException]::new("insufficient remaining release budget")
    }
    return [int][Math]::Floor($remaining)
}

function Invoke-BoundedProcess {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, [Parameter(Mandatory)][string]$FileName, [string[]]$ArgumentList = @())
    $remaining = Assert-RemainingBudget $Context
    if ($Context.ProcessAdapter) { return & $Context.ProcessAdapter $Context $FileName $ArgumentList $remaining }
    if (-not [TeamKit.Release.WindowsJobProcess]::IsSupported) {
        throw [System.PlatformNotSupportedException]::new('bounded publisher requires Windows process containment')
    }
    $process = $null
    $deadlineCancellation = $null
    try {
        $process = [TeamKit.Release.WindowsJobProcess]::Start($FileName, [string[]]$ArgumentList)
        $deadlineMilliseconds = [Math]::Max(1, [int][Math]::Floor((Get-RemainingBudgetSeconds $Context) * 1000))
        $deadlineCancellation = [System.Threading.CancellationTokenSource]::new()
        $deadlineCancellation.CancelAfter($deadlineMilliseconds)
        $overflowSignal = [System.Threading.Tasks.TaskCompletionSource[bool]]::new([System.Threading.Tasks.TaskCreationOptions]::RunContinuationsAsynchronously)
        $stdoutTask = [TeamKit.Release.BoundedCapture]::DrainAsync($process.StandardOutput, $script:MaxCapturedProcessBytes, $overflowSignal, $deadlineCancellation.Token)
        $stderrTask = [TeamKit.Release.BoundedCapture]::DrainAsync($process.StandardError, $script:MaxCapturedProcessBytes, $overflowSignal, $deadlineCancellation.Token)
        $exitTask = $process.Process.WaitForExitAsync()
        $deadlineTask = [System.Threading.Tasks.Task]::Delay($deadlineMilliseconds)
        $completeTask = [System.Threading.Tasks.Task]::WhenAll([System.Threading.Tasks.Task[]]@($exitTask, $stdoutTask, $stderrTask))
        $winner = [System.Threading.Tasks.Task]::WhenAny([System.Threading.Tasks.Task[]]@($completeTask, $deadlineTask, $overflowSignal.Task)).GetAwaiter().GetResult()
        if ($winner -eq $deadlineTask) {
            $deadlineCancellation.Cancel()
            try { $process.TerminateTree() } catch { }
            try { $process.StandardOutput.Close() } catch { }
            try { $process.StandardError.Close() } catch { }
            throw [System.TimeoutException]::new('bounded process deadline')
        }
        if ($winner -eq $overflowSignal.Task) {
            $deadlineCancellation.Cancel()
            try { $process.TerminateTree() } catch { }
            try { $process.StandardOutput.Close() } catch { }
            try { $process.StandardError.Close() } catch { }
            throw [System.InvalidOperationException]::new('bounded process output overflow')
        }
        try { $completeTask.GetAwaiter().GetResult() } catch [System.OperationCanceledException] {
            if ((Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('bounded process deadline') }
            throw [System.InvalidOperationException]::new('bounded process stream failed')
        }
        $stdoutCapture = $stdoutTask.GetAwaiter().GetResult(); $stderrCapture = $stderrTask.GetAwaiter().GetResult()
        if ($stdoutCapture.Overflowed -or $stderrCapture.Overflowed) { throw [System.InvalidOperationException]::new('bounded process output overflow') }
        if ($process.Process.ExitCode -ne 0) { throw "process failed: $FileName exit=$($process.Process.ExitCode)" }
        return [pscustomobject]@{ ExitCode = $process.Process.ExitCode; StdOut = $stdoutCapture.Text; StdErr = $stderrCapture.Text }
    } finally {
        if ($deadlineCancellation) { $deadlineCancellation.Dispose() }
        if ($process) { $process.Dispose() }
    }
}

function Invoke-ReleaseTransport {
    param($Context, $RequestShape)
    if ($Context.HttpAdapter) { return & $Context.HttpAdapter $Context $RequestShape }
    $handler = [System.Net.Http.HttpClientHandler]::new(); $handler.AllowAutoRedirect = $false
    $client = [System.Net.Http.HttpClient]::new($handler); $client.Timeout = [TimeSpan]::FromSeconds($RequestShape.TimeoutSeconds)
    $methodName = [string]$RequestShape.Method
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::$methodName, $RequestShape.Url)
    $response = $null; $stream = $null
    $cancellation = [System.Threading.CancellationTokenSource]::new()
    $cancellation.CancelAfter([TimeSpan]::FromSeconds($RequestShape.TimeoutSeconds))
    foreach ($key in $RequestShape.Headers.Keys) { [void]$request.Headers.TryAddWithoutValidation($key, [string]$RequestShape.Headers[$key]) }
    [byte[]]$body = [byte[]]::new(0)
    if ($null -ne $RequestShape.BodyUtf8) { $body = [byte[]]$RequestShape.BodyUtf8 }
    if ($body.Length -gt 0) {
        $request.Content = [System.Net.Http.ByteArrayContent]::new($body)
        $contentType = Get-ReleaseObjectValue $RequestShape 'ContentType'
        if ([string]::IsNullOrWhiteSpace([string]$contentType)) { $contentType = 'application/json' }
        $request.Content.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::new([string]$contentType)
    }
    try {
        $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancellation.Token).GetAwaiter().GetResult()
        $headers = @{}; foreach ($header in $response.Headers) { $headers[$header.Key] = @($header.Value)[0] }
        $stream = $response.Content.ReadAsStream($cancellation.Token)
        $bytes = [TeamKit.Release.BoundedCapture]::ReadBoundedAsync($stream, $RequestShape.MaxResponseBytes, $cancellation.Token).GetAwaiter().GetResult()
        return [pscustomobject]@{ StatusCode = [int]$response.StatusCode; Headers = $headers; BodyUtf8 = $bytes }
    } catch [System.OperationCanceledException] {
        if ((Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('HTTP deadline') }
        throw [System.InvalidOperationException]::new('HTTP transport failed')
    } catch {
        if ($_.Exception -is [System.TimeoutException] -and (Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('HTTP deadline') }
        throw [System.InvalidOperationException]::new('HTTP transport failed')
    } finally {
        if ($stream) { $stream.Dispose() }; if ($response) { $response.Dispose() }; $request.Dispose(); $client.Dispose(); $handler.Dispose(); $cancellation.Dispose()
    }
}

function Invoke-ReleaseHttpRaw {
    param($Context, [string]$Method, [string]$Url, [hashtable]$Headers, $Body)
    $seconds = [Math]::Min(30, (Assert-RemainingBudget $Context))
    [byte[]]$bodyBytes = if ($null -eq $Body) { [byte[]]@() } else { [Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Depth 8 -Compress)) }
    $requestShape = [pscustomobject]@{ Method = $Method; Url = $Url; Headers = $Headers; BodyUtf8 = $bodyBytes; TimeoutSeconds = $seconds; MaxResponseBytes = 1048576; Purpose = 'api'; AllowRedirect = $false }
    return Invoke-ReleaseTransport $Context $requestShape
}

function Invoke-ReleaseHttp {
    param($Context, [string]$Method, [string]$Url, [hashtable]$Headers, $Body)
    return Convert-ReleaseHttpResponse (Invoke-ReleaseHttpRaw $Context $Method $Url $Headers $Body)
}

function Convert-ReleaseHttpResponse {
    param($Response)
    $status = [int]$Response.StatusCode
    if ($status -lt 200 -or $status -ge 300) { throw [System.InvalidOperationException]::new("HTTP operation failed status=$status") }
    [byte[]]$bytes = [byte[]]$Response.BodyUtf8
    if ($null -eq $bytes -or $bytes.Length -eq 0) { return @{} }
    $content = [Text.Encoding]::UTF8.GetString($bytes)
    try { return $content | ConvertFrom-Json -AsHashtable } catch { return @{ content = $content } }
}

function Invoke-GitHubApi {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, [Parameter(Mandatory)][string]$Method, [Parameter(Mandatory)][string]$Path, $Body)
    return Convert-ReleaseHttpResponse (Invoke-GitHubApiRaw $Context $Method $Path $Body)
}

function Invoke-GitHubApiRaw {
    param($Context, [string]$Method, [string]$Path, $Body)
    if ([string]::IsNullOrWhiteSpace($Context.GitHubToken)) { throw 'GH_TOKEN is required' }
    return Invoke-ReleaseHttpRaw $Context $Method ("https://api.github.com" + $Path) @{ Authorization = "Bearer $($Context.GitHubToken)"; Accept = 'application/vnd.github+json'; 'User-Agent' = 'teamkit-bounded-release'; 'X-GitHub-Api-Version' = '2022-11-28' } $Body
}

function Invoke-GitLabApi {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, [Parameter(Mandatory)][string]$Method, [Parameter(Mandatory)][string]$Path, $Body)
    return Convert-ReleaseHttpResponse (Invoke-GitLabApiRaw $Context $Method $Path $Body)
}

function Invoke-GitLabApiRaw {
    param($Context, [string]$Method, [string]$Path, $Body)
    if ([string]::IsNullOrWhiteSpace($Context.GitLabToken)) { throw 'GITLAB_TOKEN is required' }
    return Invoke-ReleaseHttpRaw $Context $Method ("$($Context.GitLabBaseUrl)/api/v4" + $Path) @{ 'PRIVATE-TOKEN' = $Context.GitLabToken } $Body
}

function Assert-AuthorityResponseStatus {
    param([string]$Provider, $Response)
    $status = [int]$Response.StatusCode
    if ($status -eq 200) { return }
    $kind = switch ($status) {
        401 { 'unauthorized' }
        403 { 'forbidden' }
        404 { 'not-found' }
        default { 'unexpected-status' }
    }
    throw "$Provider API authority probe $kind"
}

function Assert-ReadOnlyApiAuthority {
    param($Context)
    $githubRepoResponse = Invoke-GitHubApiRaw $Context 'GET' "/repos/$($Context.GitHubRepository)" $null
    Assert-AuthorityResponseStatus 'GitHub' $githubRepoResponse
    $githubRepository = Convert-ReleaseHttpResponse $githubRepoResponse
    $expectedPrivate = $Context.Version -ne 'v0.1.5'
    if ($githubRepository.full_name -ne $Context.GitHubRepository -or $githubRepository.private -ne $expectedPrivate -or $githubRepository.permissions.push -ne $true) { throw 'GitHub API authority probe repository identity mismatch' }
    $scopeHeader = Get-ReleaseObjectValue (Get-ReleaseObjectValue $githubRepoResponse 'Headers') 'X-OAuth-Scopes'
    $scopes = @([string]$scopeHeader -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    if ($scopes -notcontains 'repo') { throw 'GitHub API authority probe lacks classic repo scope' }
    $githubWorkflowResponse = Invoke-GitHubApiRaw $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/workflows/release.yml" $null
    Assert-AuthorityResponseStatus 'GitHub' $githubWorkflowResponse
    $githubWorkflow = Convert-ReleaseHttpResponse $githubWorkflowResponse
    if ($githubWorkflow.path -ne '.github/workflows/release.yml' -or $githubWorkflow.state -ne 'active') { throw 'GitHub API authority probe release workflow mismatch' }
    $githubCIWorkflowResponse = Invoke-GitHubApiRaw $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/workflows/ci.yml" $null
    Assert-AuthorityResponseStatus 'GitHub' $githubCIWorkflowResponse
    $githubCIWorkflow = Convert-ReleaseHttpResponse $githubCIWorkflowResponse
    if ($githubCIWorkflow.path -ne '.github/workflows/ci.yml' -or $githubCIWorkflow.state -ne 'active') { throw 'GitHub API authority probe CI workflow mismatch' }

    $gitlabTokenResponse = Invoke-GitLabApiRaw $Context 'GET' '/personal_access_tokens/self' $null
    Assert-AuthorityResponseStatus 'GitLab' $gitlabTokenResponse
    $gitlabToken = Convert-ReleaseHttpResponse $gitlabTokenResponse
    if ($gitlabToken.active -ne $true -or $gitlabToken.revoked -eq $true -or @($gitlabToken.scopes) -notcontains 'api') { throw 'GitLab API authority probe token metadata mismatch' }
    $expiresAt = Get-ReleaseObjectValue $gitlabToken 'expires_at'
    if (-not [string]::IsNullOrWhiteSpace([string]$expiresAt)) {
        $expiry = $null
        try { $expiry = [datetime]::Parse([string]$expiresAt, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal).ToUniversalTime() } catch { throw 'GitLab API authority probe token expiry is malformed' }
        if ([string]$expiresAt -match '^\d{4}-\d{2}-\d{2}$') { $expiry = $expiry.AddDays(1) }
        if ($expiry -le (Get-ReleaseUtcNow $Context).AddSeconds((Get-RemainingBudgetSeconds $Context))) { throw 'GitLab API authority probe token expires before the release deadline' }
    }
    $gitlabUserResponse = Invoke-GitLabApiRaw $Context 'GET' '/user' $null
    Assert-AuthorityResponseStatus 'GitLab' $gitlabUserResponse
    $gitlabUser = Convert-ReleaseHttpResponse $gitlabUserResponse
    if ($null -eq $gitlabToken.user_id -or [string]$gitlabToken.user_id -ne [string]$gitlabUser.id) { throw 'GitLab API authority probe token user binding mismatch' }
    $gitlabProjectResponse = Invoke-GitLabApiRaw $Context 'GET' "/projects/$($Context.GitLabProjectId)" $null
    Assert-AuthorityResponseStatus 'GitLab' $gitlabProjectResponse
    $gitlabProject = Convert-ReleaseHttpResponse $gitlabProjectResponse
    if ([string]$gitlabProject.id -ne [string]$Context.GitLabProjectId -or $gitlabProject.path_with_namespace -ne '1c/aisuz/ai' -or $gitlabProject.archived -ne $false -or (Get-EffectiveGitLabProjectAccessLevel $gitlabProject) -lt 40) { throw 'GitLab API authority probe project identity or Maintainer access mismatch' }
    return @{ GitHubRepository = $githubRepository; GitLabProject = $gitlabProject }
}

function Invoke-ReleaseStep {
    param($Context, [string]$Name, $Data)
    $Context.State.CurrentStage = $Name
    $Context.State.FailureReasonCode = $null
    [void]$Context.Events.Add($Name)
    try {
        if ($Context.OrderAdapter) { & $Context.OrderAdapter $Context $Name $Data }
        return Invoke-ProductionReleaseStep $Context $Name $Data
    } catch [System.TimeoutException] {
        throw
    } catch {
        if ([string]::IsNullOrWhiteSpace([string]$Context.State.FailureReasonCode)) {
            $Context.State.FailureReasonCode = (($Name -replace '[^A-Za-z0-9]', '_').ToUpperInvariant() + '_FAILED')
        }
        throw
    }
}

function Invoke-ReleaseSleep {
    param($Context, [int]$Seconds)
    $bounded = [Math]::Min([Math]::Min(10, [Math]::Max(1, $Seconds)), (Assert-RemainingBudget $Context))
    if ($Context.SleepAdapter) { & $Context.SleepAdapter $Context $bounded; return }
    Start-Sleep -Seconds $bounded
}

function Invoke-ReleasePreflight {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    return Invoke-ReleaseStep $Context 'preflight' $null
}

function Sync-ReleaseRefs {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    return Invoke-ReleaseStep $Context 'sync-refs' $null
}

function Wait-ExactShaCI {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    return Invoke-ReleaseStep $Context 'wait-ci' $null
}

function Compare-PublicationSets {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, $CI)
    return Invoke-ReleaseStep $Context 'compare-six' $CI
}

function Publish-ProtectedTag {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, $CI)
    Invoke-ReleaseStep $Context 'protect-tag' $CI | Out-Null
    return Invoke-ReleaseStep $Context 'tag' $CI
}

function Publish-GitLabRelease {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, $CI, $Tag)
    return Invoke-ReleaseStep $Context 'release' @{ CI = $CI; Tag = $Tag }
}

function Test-PublishedRelease {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context, $CI, $Release, $Tag)
    return Invoke-ReleaseStep $Context 'verify-eight' @{ CI = $CI; Release = $Release; Tag = $Tag }
}

function Get-ExactPipeline {
    param($Context)
    $deadline = Get-RemainingSeconds $Context
    while ($deadline -gt 0) {
        $probe = Get-PairedCIProbe $Context
        $github = $probe.github; $gitlab = $probe.gitlab
        $refSyncStartedAt = if ($Context.State.ContainsKey('RefSyncStartedAt')) { $Context.State.RefSyncStartedAt } else { $null }
        $githubBaselineIds = if ($Context.State.ContainsKey('CIGitHubBaselineIds')) { $Context.State.CIGitHubBaselineIds } else { @{} }
        $gitlabBaselineIds = if ($Context.State.ContainsKey('CIGitLabBaselineIds')) { $Context.State.CIGitLabBaselineIds } else { @{} }
        $expectedGitHubEvent = if ($Context.State.ContainsKey('ExpectedGitHubCIEvent')) { $Context.State.ExpectedGitHubCIEvent } else { $null }
        $expectedGitLabSource = if ($Context.State.ContainsKey('ExpectedGitLabCISource')) { $Context.State.ExpectedGitLabCISource } else { $null }
        $expectedGitLabPipelineId = if ($Context.State.ContainsKey('ExpectedGitLabPipelineId')) { $Context.State.ExpectedGitLabPipelineId } else { $null }
        $ghCandidates = @($github.workflow_runs | Where-Object {
            $createdAt = ConvertTo-ReleaseUtcTimestamp $_.created_at
            $_.path -eq '.github/workflows/ci.yml' -and $_.head_sha -eq $Context.CandidateSha -and $_.head_branch -eq $Context.GitHubBranch -and $_.event -in @('push', 'workflow_dispatch') -and ($null -eq $expectedGitHubEvent -or $_.event -eq $expectedGitHubEvent) -and $null -ne $createdAt -and ($null -eq $refSyncStartedAt -or $createdAt -ge $refSyncStartedAt) -and -not $githubBaselineIds.ContainsKey([string]$_.id)
        })
        $glCandidates = @($gitlab | Where-Object {
            $createdAt = ConvertTo-ReleaseUtcTimestamp $_.created_at
            $_.sha -eq $Context.CandidateSha -and $_.ref -eq $Context.GitLabBranch -and $_.source -in @('push', 'web', 'api') -and ($null -eq $expectedGitLabSource -or $_.source -eq $expectedGitLabSource) -and ($null -eq $expectedGitLabPipelineId -or [string]$_.id -eq [string]$expectedGitLabPipelineId) -and ($null -eq $refSyncStartedAt -or ($null -ne $createdAt -and $createdAt -ge $refSyncStartedAt)) -and -not $gitlabBaselineIds.ContainsKey([string]$_.id)
        })
        if ($ghCandidates.Count -gt 1) { throw 'ambiguous exact-SHA GitHub CI runs' }
        if ($glCandidates.Count -gt 1) { throw 'ambiguous exact-SHA GitLab pipelines' }
        $ghRun = if ($ghCandidates.Count -eq 1) { $ghCandidates[0] } else { $null }
        $glPipeline = if ($glCandidates.Count -eq 1) { $glCandidates[0] } else { $null }
        # Evaluate both providers before any pending-state sleep. A terminal
        # failure is decisive even when its counterpart is still queued.
        $githubStatus = [string](Get-ReleaseObjectValue $ghRun 'status')
        $githubConclusion = [string](Get-ReleaseObjectValue $ghRun 'conclusion')
        $gitlabStatus = [string](Get-ReleaseObjectValue $glPipeline 'status')
        $githubTerminalFailure = $ghRun -and $githubStatus -eq 'completed' -and $githubConclusion -ne 'success'
        $gitlabTerminalFailure = $glPipeline -and $gitlabStatus -ne 'success' -and $gitlabStatus -notin @('created', 'waiting_for_resource', 'preparing', 'pending', 'running', 'scheduled')
        if ($githubTerminalFailure) { throw "exact GitHub CI concluded $githubConclusion" }
        if ($gitlabTerminalFailure) { throw "exact GitLab CI concluded $gitlabStatus" }
        $githubPending = $ghRun -and $githubConclusion -ne 'success'
        $gitlabPending = $glPipeline -and $gitlabStatus -ne 'success'
        if ($githubPending -or $gitlabPending) {
            Invoke-ReleaseSleep $Context 10; $deadline = Get-RemainingSeconds $Context; continue
        }
        if ($ghRun -and $glPipeline) {
            $jobs = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/pipelines/$($glPipeline.id)/jobs?per_page=100" $null
            $verify = @($jobs | Where-Object { $_.name -eq 'verify' -and (Get-ReleaseObjectValue (Get-ReleaseObjectValue $_ 'commit') 'id') -eq $Context.CandidateSha })
            if ($verify.Count -gt 1) { throw 'ambiguous exact GitLab verify jobs' }
            if ($verify.Count -eq 1) {
                if ($verify[0].status -eq 'success') { return @{ github_run_id = $ghRun.id; gitlab_pipeline_id = $glPipeline.id; gitlab_job_id = $verify[0].id } }
                if ($verify[0].status -notin @('created', 'waiting_for_resource', 'preparing', 'pending', 'running', 'scheduled')) { throw "exact GitLab verify job concluded $($verify[0].status)" }
            }
        }
        Invoke-ReleaseSleep $Context 10
        $deadline = Get-RemainingSeconds $Context
    }
    throw [System.TimeoutException]::new('exact-SHA CI did not finish')
}

function Get-VerifiedExactCI {
    param($Context)
    $expected = $Context.VerifiedCI
    if (-not $expected) { throw 'verified CI IDs are unavailable' }
    $pipeline = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/pipelines/$($expected.gitlab_pipeline_id)" $null
    $pipelineSource = Get-ReleaseObjectValue $pipeline 'source'
    if ([string]$pipeline.id -ne [string]$expected.gitlab_pipeline_id -or $pipeline.sha -ne $Context.CandidateSha -or $pipeline.ref -ne $Context.GitLabBranch -or $pipelineSource -notin @('push', 'web', 'api') -or $pipeline.status -ne 'success') { throw 'provided GitLab pipeline is not exact candidate evidence' }
    $handoff = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/jobs/$($expected.gitlab_handoff_job_id)" $null
    $verify = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/jobs/$($expected.gitlab_job_id)" $null
    $proofs = if ([string]$expected.gitlab_handoff_job_id -eq [string]$expected.gitlab_job_id) { @(@{ job = $verify; id = $expected.gitlab_job_id; name = 'verify' }) } else { @(@{ job = $handoff; id = $expected.gitlab_handoff_job_id; name = 'release-handoff' }, @{ job = $verify; id = $expected.gitlab_job_id; name = 'verify' }) }
    foreach ($proof in $proofs) {
        $jobPipelineId = Get-ReleaseObjectValue (Get-ReleaseObjectValue $proof.job 'pipeline') 'id'
        if ([string]$proof.job.id -ne [string]$proof.id -or $proof.job.name -ne $proof.name -or $proof.job.status -ne 'success' -or (Get-ReleaseObjectValue (Get-ReleaseObjectValue $proof.job 'commit') 'id') -ne $Context.CandidateSha -or [string]$jobPipelineId -ne [string]$expected.gitlab_pipeline_id -or [string]::IsNullOrWhiteSpace([string](Get-ReleaseObjectValue (Get-ReleaseObjectValue $proof.job 'artifacts_file') 'filename'))) { throw "provided GitLab $($proof.name) job is not exact candidate evidence" }
    }
    return @{ gitlab_pipeline_id = $pipeline.id; gitlab_handoff_job_id = $handoff.id; gitlab_job_id = $verify.id }
}

function Get-PairedCIProbe {
    param($Context)
    $seconds = [Math]::Min(30, (Assert-RemainingBudget $Context))
    $ghUrl = "https://api.github.com/repos/$($Context.GitHubRepository)/actions/workflows/ci.yml/runs?head_sha=$($Context.CandidateSha)&per_page=20"
    $glUrl = "$($Context.GitLabBaseUrl)/api/v4/projects/$($Context.GitLabProjectId)/pipelines?sha=$($Context.CandidateSha)&per_page=20"
    $githubRequest = [pscustomobject]@{ Method = 'GET'; Url = $ghUrl; Headers = @{ Authorization = "Bearer $($Context.GitHubToken)"; Accept = 'application/vnd.github+json'; 'User-Agent' = 'teamkit-bounded-release' }; BodyUtf8 = [byte[]]@(); TimeoutSeconds = $seconds; MaxResponseBytes = 1048576; Purpose = 'ci-github'; AllowRedirect = $false }
    $gitlabRequest = [pscustomobject]@{ Method = 'GET'; Url = $glUrl; Headers = @{ 'PRIVATE-TOKEN' = $Context.GitLabToken }; BodyUtf8 = [byte[]]@(); TimeoutSeconds = $seconds; MaxResponseBytes = 1048576; Purpose = 'ci-gitlab'; AllowRedirect = $false }
    if ($Context.PairedCIAdapter) {
        $pair = & $Context.PairedCIAdapter $Context $githubRequest $gitlabRequest
        return @{ github = (Convert-ReleaseHttpResponse $pair.github); gitlab = (Convert-ReleaseHttpResponse $pair.gitlab) }
    }
    # Each job owns only a single read request.  They are started before either
    # result is awaited, so an unavailable provider cannot serialize CI polling.
    $fetch = {
        param([string]$Url, [string]$HeaderName, [string]$Token, [int]$TimeoutSeconds)
        $handler = [System.Net.Http.HttpClientHandler]::new(); $handler.AllowAutoRedirect = $false
        $client = [System.Net.Http.HttpClient]::new($handler); $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
        $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, $Url)
        $response = $null; $stream = $null; $cancellation = [System.Threading.CancellationTokenSource]::new()
        $cancellation.CancelAfter([TimeSpan]::FromSeconds($TimeoutSeconds))
        try {
            [void]$request.Headers.TryAddWithoutValidation($HeaderName, $Token)
            [void]$request.Headers.TryAddWithoutValidation('User-Agent', 'teamkit-bounded-release')
            $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancellation.Token).GetAwaiter().GetResult()
            $headers = @{}; foreach ($header in $response.Headers) { $headers[$header.Key] = @($header.Value)[0] }
            $stream = $response.Content.ReadAsStream($cancellation.Token)
            $bytes = [TeamKit.Release.BoundedCapture]::ReadBoundedAsync($stream, 1048576, $cancellation.Token).GetAwaiter().GetResult()
            return [pscustomobject]@{ StatusCode = [int]$response.StatusCode; Headers = $headers; BodyUtf8 = $bytes }
        } catch {
            throw 'CI probe transport failed'
        } finally {
            if ($stream) { $stream.Dispose() }; if ($response) { $response.Dispose() }
            $request.Dispose(); $client.Dispose(); $handler.Dispose(); $cancellation.Dispose()
        }
    }
    $ghJob = Start-ThreadJob -ScriptBlock $fetch -ArgumentList $ghUrl, 'Authorization', ("Bearer $($Context.GitHubToken)"), $seconds
    $glJob = Start-ThreadJob -ScriptBlock $fetch -ArgumentList $glUrl, 'PRIVATE-TOKEN', $Context.GitLabToken, $seconds
    $waitSeconds = [Math]::Min($seconds, (Assert-RemainingBudget $Context))
    try {
        Wait-Job -Job $ghJob,$glJob -Timeout $waitSeconds | Out-Null
        $jobs = @($ghJob, $glJob)
        $unfinishedJobs = @($jobs | Where-Object { $_.State -in @('NotStarted', 'Running', 'Stopping', 'Suspended', 'Blocked') })
        if ($unfinishedJobs.Count -ne 0) {
            if ((Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('parallel CI probe deadline') }
            throw [System.InvalidOperationException]::new('parallel CI probe did not finish within bounded request')
        }
        $failedJobs = @($jobs | Where-Object { $_.State -ne 'Completed' })
        if ($failedJobs.Count -ne 0) {
            foreach ($job in $failedJobs) { Receive-Job -Job $job -ErrorAction SilentlyContinue | Out-Null }
            if ((Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('parallel CI probe deadline') }
            throw [System.InvalidOperationException]::new('parallel CI probe failed')
        }
        $ghRaw = Receive-Job -Job $ghJob -ErrorAction Stop
        $glRaw = Receive-Job -Job $glJob -ErrorAction Stop
        return @{ github = (Convert-ReleaseHttpResponse $ghRaw); gitlab = (Convert-ReleaseHttpResponse $glRaw) }
    } finally {
        foreach ($job in @($ghJob, $glJob)) { if ($job.State -eq 'Running') { Stop-Job -Job $job -ErrorAction SilentlyContinue }; Remove-Job -Job $job -Force -ErrorAction SilentlyContinue }
    }
}

function Wait-ReleaseDownloadNotBefore {
    param($Context, [Uri]$Target)
    $match = [regex]::Match($Target.Query, '(?:^|[?&])st=([^&]+)')
    if (-not $match.Success) { return }
    $notBefore = ConvertTo-ReleaseUtcTimestamp ([Uri]::UnescapeDataString($match.Groups[1].Value))
    if ($null -eq $notBefore) { return }
    while ($true) {
        $remaining = ($notBefore - (Get-ReleaseUtcNow $Context)).TotalSeconds
        if ($remaining -le 0) { return }
        Invoke-ReleaseSleep $Context ([int][Math]::Ceiling($remaining))
    }
}

function Save-ReleaseDownload {
    param(
        $Context,
        [string]$Url,
        [string]$Path,
        [hashtable]$Headers = @{},
        [switch]$AllowRedirect,
        [ValidateRange(0, 3)][int]$RedirectCount = 0,
        [string]$RetryOriginUrl,
        [hashtable]$RetryOriginHeaders,
        [ValidateRange(0, 2)][int]$ForbiddenRetryCount = 0
    )
    if ([string]::IsNullOrWhiteSpace($RetryOriginUrl)) { $RetryOriginUrl = $Url; $RetryOriginHeaders = $Headers }
    $seconds = [Math]::Min(30, (Assert-RemainingBudget $Context))
    $requestShape = [pscustomobject]@{ Method = 'GET'; Url = $Url; Headers = $Headers; BodyUtf8 = [byte[]]@(); TimeoutSeconds = $seconds; MaxResponseBytes = $script:MaxPublicationArchiveCompressedBytes; Purpose = 'download'; AllowRedirect = [bool]$AllowRedirect }
    if ($Context.HttpAdapter) {
        $response = Invoke-ReleaseTransport $Context $requestShape
        if ($AllowRedirect -and [int]$response.StatusCode -in 301,302,303,307,308) {
            if ($RedirectCount -ge 3) { throw 'artifact redirect limit exceeded' }
            $target = [Uri]$response.Headers['Location']
            if (-not $target -or $target.Scheme -ne 'https') { throw 'artifact redirect is not HTTPS' }
            Wait-ReleaseDownloadNotBefore $Context $target
            return Save-ReleaseDownload $Context $target.AbsoluteUri $Path @{} -AllowRedirect -RedirectCount ($RedirectCount + 1) -RetryOriginUrl $RetryOriginUrl -RetryOriginHeaders $RetryOriginHeaders -ForbiddenRetryCount $ForbiddenRetryCount
        }
        if ($AllowRedirect -and [int]$response.StatusCode -eq 403 -and $RedirectCount -gt 0 -and $ForbiddenRetryCount -lt 2) {
            Invoke-ReleaseSleep $Context 10
            return Save-ReleaseDownload $Context $RetryOriginUrl $Path $RetryOriginHeaders -AllowRedirect -RetryOriginUrl $RetryOriginUrl -RetryOriginHeaders $RetryOriginHeaders -ForbiddenRetryCount ($ForbiddenRetryCount + 1)
        }
        if ([int]$response.StatusCode -lt 200 -or [int]$response.StatusCode -ge 300) { throw [System.InvalidOperationException]::new("download operation failed status=$($response.StatusCode)") }
        [byte[]]$bytes = [byte[]]$response.BodyUtf8
        if ($bytes.Length -gt $script:MaxPublicationArchiveCompressedBytes) { throw 'artifact download exceeds compressed size bound' }
        $output = [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        try { $output.Write($bytes, 0, $bytes.Length) } finally { $output.Dispose() }
        return $Path
    }

    $handler = [System.Net.Http.HttpClientHandler]::new(); $handler.AllowAutoRedirect = $false
    $client = [System.Net.Http.HttpClient]::new($handler)
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, $Url)
    $response = $null; $input = $null; $output = $null
    $cancellation = [System.Threading.CancellationTokenSource]::new()
    $cancellation.CancelAfter([TimeSpan]::FromSeconds($seconds))
    foreach ($key in $Headers.Keys) { [void]$request.Headers.TryAddWithoutValidation($key, [string]$Headers[$key]) }
    try {
        $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancellation.Token).GetAwaiter().GetResult()
        if ($AllowRedirect -and [int]$response.StatusCode -in 301,302,303,307,308) {
            if ($RedirectCount -ge 3) { throw 'artifact redirect limit exceeded' }
            $location = $response.Headers.Location
            if ($null -eq $location -or -not $location.IsAbsoluteUri -or $location.Scheme -ne 'https') { throw 'artifact redirect is not HTTPS' }
            Wait-ReleaseDownloadNotBefore $Context $location
            return Save-ReleaseDownload $Context $location.AbsoluteUri $Path @{} -AllowRedirect -RedirectCount ($RedirectCount + 1) -RetryOriginUrl $RetryOriginUrl -RetryOriginHeaders $RetryOriginHeaders -ForbiddenRetryCount $ForbiddenRetryCount
        }
        if ($AllowRedirect -and [int]$response.StatusCode -eq 403 -and $RedirectCount -gt 0 -and $ForbiddenRetryCount -lt 2) {
            Invoke-ReleaseSleep $Context 10
            return Save-ReleaseDownload $Context $RetryOriginUrl $Path $RetryOriginHeaders -AllowRedirect -RetryOriginUrl $RetryOriginUrl -RetryOriginHeaders $RetryOriginHeaders -ForbiddenRetryCount ($ForbiddenRetryCount + 1)
        }
        if (-not $response.IsSuccessStatusCode) { throw [System.InvalidOperationException]::new("download operation failed status=$([int]$response.StatusCode)") }
        $length = $response.Content.Headers.ContentLength
        if ($null -ne $length -and [int64]$length -gt $script:MaxPublicationArchiveCompressedBytes) { throw 'artifact download exceeds compressed size bound' }
        $input = $response.Content.ReadAsStream($cancellation.Token)
        $output = [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        $buffer = New-Object byte[] 81920
        [int64]$total = 0
        while ($true) {
            Assert-RemainingBudget $Context | Out-Null
            $read = $input.ReadAsync($buffer, 0, $buffer.Length, $cancellation.Token).GetAwaiter().GetResult()
            if ($read -eq 0) { break }
            $total += $read
            if ($total -gt $script:MaxPublicationArchiveCompressedBytes) { throw 'artifact download exceeds compressed size bound' }
            $output.Write($buffer, 0, $read)
        }
        return $Path
    } catch [System.OperationCanceledException] {
        if ((Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('artifact download deadline') }
        throw [System.InvalidOperationException]::new('artifact download transport failed')
    } catch {
        if ($_.Exception -is [System.InvalidOperationException]) { throw }
        if ($_.Exception -is [System.TimeoutException] -and (Get-RemainingBudgetSeconds $Context) -le 0) { throw [System.TimeoutException]::new('artifact download deadline') }
        throw [System.InvalidOperationException]::new('artifact download transport failed')
    } finally {
        if ($output) { $output.Dispose() }; if ($input) { $input.Dispose() }; if ($response) { $response.Dispose() }
        $request.Dispose(); $client.Dispose(); $handler.Dispose(); $cancellation.Dispose()
    }
}

function Expand-PublicationArchive {
    param($Context, [string]$Archive, [string]$Name, [ValidateSet('dist', 'root')][string]$Layout = 'dist')
    $root = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-release-" + [Guid]::NewGuid().ToString('N'))
    [void][System.IO.Directory]::CreateDirectory($root)
    $files = @{}
    $input = [System.IO.File]::Open($Archive, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::Read)
    try {
        $zip = [System.IO.Compression.ZipArchive]::new($input, [System.IO.Compression.ZipArchiveMode]::Read, $false)
        try {
            [long]$compressed = 0; [long]$uncompressed = 0
            [long]$actualUncompressed = 0
            $entries = @{}; $seenEntryNames = @{}
            $entryPattern = if ($Layout -eq 'dist') { '^dist/[^/]+$' } else { '^[^/]+$' }
            $entryPrefix = if ($Layout -eq 'dist') { 'dist/' } else { '' }
            foreach ($entry in $zip.Entries) {
                Assert-RemainingBudget $Context | Out-Null
                $entryName = $entry.FullName.Replace('\', '/')
                if ($seenEntryNames.ContainsKey($entryName)) { throw "$Name artifact ZIP has duplicate entry" }
                $seenEntryNames[$entryName] = $true
                $unixType = (($entry.ExternalAttributes -shr 16) -band 0xF000)
                if ($unixType -eq 0xA000) { throw "$Name artifact ZIP has symlink-like entry" }
                if ($entryName.EndsWith('/')) {
                    if ($Layout -ne 'dist' -or $entryName -ne 'dist/' -or $entry.Length -ne 0 -or $entry.CompressedLength -ne 0) { throw "$Name artifact ZIP has unsafe directory entry" }
                    continue
                }
                if ($entryName -notmatch $entryPattern -or $entryName.Contains('..') -or $entryName.StartsWith('/') -or $entryName.Contains(':')) { throw "$Name artifact ZIP has unsafe entry path" }
                $compressed += $entry.CompressedLength; $uncompressed += $entry.Length
                if ($compressed -gt $script:MaxPublicationArchiveCompressedBytes -or $uncompressed -gt $script:MaxPublicationArchiveUncompressedBytes) { throw "$Name artifact ZIP exceeds size bound" }
                $entries[$entryName] = $entry
            }
            if ($entries.Count -ne $Context.ReleaseFiles.Count) { throw "$Name artifact ZIP must contain exactly six regular entries" }
            foreach ($required in $Context.ReleaseFiles) {
                $entryName = "$entryPrefix$required"
                if (-not $entries.ContainsKey($entryName)) { throw "$Name artifact ZIP is missing $required" }
                $entry = $entries[$entryName]
                $destination = Join-Path $root $required
                $source = $entry.Open(); $output = [System.IO.File]::Open($destination, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
                try {
                    $buffer = New-Object byte[] 81920
                    [long]$actualEntryBytes = 0
                    while (($read = $source.Read($buffer, 0, $buffer.Length)) -gt 0) {
                        Assert-RemainingBudget $Context | Out-Null
                        $actualEntryBytes += $read; $actualUncompressed += $read
                        if ($actualEntryBytes -gt $entry.Length -or $actualUncompressed -gt $script:MaxPublicationArchiveUncompressedBytes) { throw "$Name artifact ZIP actual extracted size exceeds bound" }
                        $output.Write($buffer, 0, $read)
                    }
                    if ($actualEntryBytes -ne $entry.Length) { throw "$Name artifact ZIP actual extracted size does not match central directory" }
                } finally { $output.Dispose(); $source.Dispose() }
                $files[$required] = $destination
            }
        } finally { $zip.Dispose() }
    } finally { $input.Dispose() }
    return $files
}

function Expand-HandoffPublicationArchive {
    param($Context, [string]$Archive)
    $root = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-handoff-" + [Guid]::NewGuid().ToString('N'))
    [void][System.IO.Directory]::CreateDirectory($root)
    $expected = @($Context.ReleaseFiles | ForEach-Object { "handoff/$_" }) + @('handoff/MANIFEST.json', 'handoff/SHA256SUMS.handoff')
    $entries = @{}; $seen = @{}
    $input = [System.IO.File]::Open($Archive, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::Read)
    try {
        $zip = [System.IO.Compression.ZipArchive]::new($input, [System.IO.Compression.ZipArchiveMode]::Read, $false)
        try {
            [long]$compressed = 0; [long]$uncompressed = 0; [long]$actual = 0
            foreach ($entry in $zip.Entries) {
                Assert-RemainingBudget $Context | Out-Null
                $name = $entry.FullName.Replace('\', '/')
                if ($seen.ContainsKey($name)) { throw 'GitLab handoff ZIP has duplicate entry' }
                $seen[$name] = $true
                if ((($entry.ExternalAttributes -shr 16) -band 0xF000) -eq 0xA000) { throw 'GitLab handoff ZIP has symlink-like entry' }
                if ($name.EndsWith('/')) {
                    if ($name -ne 'handoff/' -or $entry.Length -ne 0 -or $entry.CompressedLength -ne 0) { throw 'GitLab handoff ZIP has unsafe directory entry' }
                    continue
                }
                if ($name -notin $expected -or $name.Contains('..') -or $name.StartsWith('/') -or $name.Contains(':')) { throw 'GitLab handoff ZIP has unsafe entry path' }
                $compressed += $entry.CompressedLength; $uncompressed += $entry.Length
                if ($compressed -gt $script:MaxPublicationArchiveCompressedBytes -or $uncompressed -gt $script:MaxPublicationArchiveUncompressedBytes) { throw 'GitLab handoff ZIP exceeds size bound' }
                $entries[$name] = $entry
            }
            if ($entries.Count -ne $expected.Count) { throw 'GitLab handoff ZIP must contain exactly six files and two metadata entries' }
            foreach ($name in $expected) { if (-not $entries.ContainsKey($name)) { throw "GitLab handoff ZIP is missing $name" } }
            $files = @{}
            foreach ($name in $expected) {
                $leaf = [IO.Path]::GetFileName($name); $destination = Join-Path $root $leaf
                $source = $entries[$name].Open(); $output = [IO.File]::Open($destination, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
                try {
                    $buffer = New-Object byte[] 81920; [long]$entryActual = 0
                    while (($read = $source.Read($buffer, 0, $buffer.Length)) -gt 0) {
                        Assert-RemainingBudget $Context | Out-Null; $entryActual += $read; $actual += $read
                        if ($entryActual -gt $entries[$name].Length -or $actual -gt $script:MaxPublicationArchiveUncompressedBytes) { throw 'GitLab handoff ZIP actual extracted size exceeds bound' }
                        $output.Write($buffer, 0, $read)
                    }
                    if ($entryActual -ne $entries[$name].Length) { throw 'GitLab handoff ZIP actual extracted size does not match central directory' }
                } finally { $output.Dispose(); $source.Dispose() }
                if ($name -like 'handoff/teamkit-*' -or $name -in @('handoff/SHA256SUMS', 'handoff/SECURITY-AUDIT.json')) { $files[$leaf] = $destination }
            }
            $manifest = Get-Content -LiteralPath (Join-Path $root 'MANIFEST.json') -Raw -Encoding UTF8 | ConvertFrom-Json -AsHashtable
            if ($manifest.commit -ne $Context.CandidateSha -or $manifest.version -ne $Context.Version -or $manifest.github_run_id -notmatch '^[1-9][0-9]*$' -or $manifest.github_artifact_id -notmatch '^[1-9][0-9]*$' -or $manifest.github_artifact_digest -notmatch '^sha256:[0-9a-f]{64}$' -or $manifest.sha256_file -ne 'SHA256SUMS.handoff') { throw 'GitLab handoff manifest identity is invalid' }
            $rows = Get-Content -LiteralPath (Join-Path $root 'SHA256SUMS.handoff') -Encoding UTF8 | Where-Object { $_.Trim() }
            if ($rows.Count -ne $Context.ReleaseFiles.Count) { throw 'GitLab handoff checksum manifest must contain exactly six rows' }
            foreach ($required in $Context.ReleaseFiles) {
                $hash = (Get-FileHash -LiteralPath $files[$required] -Algorithm SHA256).Hash.ToLowerInvariant()
                if (@($rows | Where-Object { $_ -ceq "$hash  $required" }).Count -ne 1) { throw "GitLab handoff checksum mismatch: $required" }
            }
            return @{ Files = $files; Manifest = $manifest }
        } finally { $zip.Dispose() }
    } finally { $input.Dispose() }
}

function Get-PublicationHashMap {
    param($Context, [hashtable]$Files)
    $hashes = @{}
    foreach ($name in $Context.ReleaseFiles) { $hashes[$name] = (Get-FileHash -LiteralPath $Files[$name] -Algorithm SHA256).Hash.ToLowerInvariant() }
    return $hashes
}

function Get-GenericPackageUrl {
    param($Context, [string]$Name)
    $encodedName = [Uri]::EscapeDataString($Name)
    return "$($Context.GitLabBaseUrl)/api/v4/projects/$($Context.GitLabProjectId)/packages/generic/$($Context.PackageName)/$($Context.PackageVersion)/$encodedName"
}

function Invoke-GenericPackageRequest {
    param($Context, [string]$Method, [string]$Name, [byte[]]$Body = [byte[]]@())
    if ([string]::IsNullOrWhiteSpace($Context.GitLabToken)) { throw 'GITLAB_TOKEN is required' }
    $seconds = [Math]::Min(30, (Assert-RemainingBudget $Context))
    $request = [pscustomobject]@{
        Method = $Method
        Url = Get-GenericPackageUrl $Context $Name
        Headers = @{ 'PRIVATE-TOKEN' = $Context.GitLabToken }
        BodyUtf8 = $Body
        ContentType = 'application/octet-stream'
        TimeoutSeconds = $seconds
        MaxResponseBytes = 1048576
        Purpose = 'package'
        AllowRedirect = $false
    }
    return Invoke-ReleaseTransport $Context $request
}

function Get-GitLabInventoryPages {
    param($Context, [string]$Path)
    $items = [System.Collections.Generic.List[object]]::new()
    for ($page = 1; $page -le $script:MaxPackageInventoryPages; $page++) {
        $separator = if ($Path.Contains('?')) { '&' } else { '?' }
        $response = Invoke-GitLabApiRaw $Context 'GET' ($Path + $separator + "per_page=100&page=$page") $null
        if ([int]$response.StatusCode -ne 200) { throw "GitLab package inventory failed status=$($response.StatusCode)" }
        [byte[]]$body = $response.BodyUtf8
        if ($null -eq $body -or $body.Length -eq 0) { throw 'GitLab package inventory response is empty' }
        try { $values = [Text.Encoding]::UTF8.GetString($body) | ConvertFrom-Json -AsHashtable -NoEnumerate } catch { throw 'GitLab package inventory response is malformed' }
        if ($values -isnot [System.Array]) { throw 'GitLab package inventory response is not an array' }
        foreach ($value in @($values)) { if ($null -eq $value) { throw 'GitLab package inventory contains a null record' }; $items.Add($value) }
        $nextPage = Get-ReleaseObjectValue (Get-ReleaseObjectValue $response 'Headers') 'X-Next-Page'
        if ($null -eq $nextPage) { throw 'GitLab package inventory pagination is ambiguous' }
        if ([string]::IsNullOrWhiteSpace([string]$nextPage)) { return @($items) }
        if ([string]$nextPage -notmatch '^[1-9][0-9]*$' -or [int]$nextPage -ne ($page + 1)) { throw 'GitLab package inventory pagination is malformed' }
    }
    throw 'GitLab package inventory exceeds the page bound'
}

function Get-ExactGenericPackageRecords {
    param($Context)
    $records = [System.Collections.Generic.List[object]]::new()
    $seenIds = @{}
    foreach ($status in $script:GenericPackageStatuses) {
        $path = "/projects/$($Context.GitLabProjectId)/packages?package_type=generic&package_name=$([Uri]::EscapeDataString($Context.PackageName))&package_version=$([Uri]::EscapeDataString($Context.PackageVersion))&include_versionless=false&status=$([Uri]::EscapeDataString($status))"
        foreach ($record in @(Get-GitLabInventoryPages $Context $path)) {
            if ($record.name -ne $Context.PackageName -or $record.version -ne $Context.PackageVersion -or $record.package_type -ne 'generic') { continue }
            if ([string]$record.status -ne $status -or [string]$record.id -notmatch '^[1-9][0-9]*$') { throw 'exact Generic Package record is malformed' }
            if ($seenIds.ContainsKey([string]$record.id)) { throw 'duplicate Generic Package record is ambiguous' }
            $seenIds[[string]$record.id] = $true
            $records.Add($record)
        }
    }
    return @($records)
}

function Assert-GenericPackageSetAbsent {
    param($Context)
    $records = @(Get-ExactGenericPackageRecords $Context)
    if ($records.Count -ne 0) { throw 'Generic Package version already has external state' }
}

function Assert-ExactGenericPackageInventory {
    param($Context, [string[]]$ExpectedNames, [hashtable]$ExpectedHashes)
    $records = @(Get-ExactGenericPackageRecords $Context)
    if ($records.Count -ne 1 -or $records[0].status -ne 'default') { throw 'Generic Package inventory must contain one exact default record' }
    $packageId = [string]$records[0].id
    $files = @(Get-GitLabInventoryPages $Context "/projects/$($Context.GitLabProjectId)/packages/$packageId/package_files?order_by=id&sort=asc")
    if ($files.Count -ne $ExpectedNames.Count) { throw 'Generic Package inventory file count mismatch' }
    $expected = @{}; foreach ($name in $ExpectedNames) { if ($expected.ContainsKey($name)) { throw 'expected Generic Package prefix contains a duplicate' }; $expected[$name] = $true }
    $seenNames = @{}; $seenIds = @{}
    foreach ($file in $files) {
        $name = [string]$file.file_name; $fileId = [string]$file.id
        if ([string]$file.package_id -ne $packageId -or $fileId -notmatch '^[1-9][0-9]*$' -or -not $expected.ContainsKey($name)) { throw 'Generic Package file inventory contains an unexpected record' }
        if ($seenIds.ContainsKey($fileId) -or $seenNames.ContainsKey($name)) { throw 'Generic Package file inventory contains a duplicate' }
        $seenIds[$fileId] = $true; $seenNames[$name] = $true
        $apiHash = Get-ReleaseObjectValue $file 'file_sha256'
        if (-not [string]::IsNullOrWhiteSpace([string]$apiHash)) {
            if ([string]$apiHash -notmatch '^[0-9a-f]{64}$' -or -not $ExpectedHashes.ContainsKey($name) -or [string]$apiHash -ne [string]$ExpectedHashes[$name]) { throw "Generic Package API SHA-256 mismatch: $name" }
        }
    }
    foreach ($name in $ExpectedNames) { if (-not $seenNames.ContainsKey($name)) { throw "Generic Package inventory is missing: $name" } }
    return @{ Package = $records[0]; Files = $files }
}

function Test-GenericPackageSet {
    param($Context, [hashtable]$ExpectedHashes, [switch]$InventoryVerified)
    if (-not $ExpectedHashes -or $ExpectedHashes.Count -ne $Context.ReleaseFiles.Count) { throw 'exact package hashes are unavailable' }
    if (-not $InventoryVerified) { Assert-ExactGenericPackageInventory $Context @($Context.ReleaseFiles) $ExpectedHashes | Out-Null }
    $records = [System.Collections.Generic.List[object]]::new()
    foreach ($name in $Context.ReleaseFiles) {
        $path = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-package-" + [Guid]::NewGuid().ToString('N'))
        try {
            $url = Get-GenericPackageUrl $Context $name
            Save-ReleaseDownload $Context $url $path @{ 'PRIVATE-TOKEN' = $Context.GitLabToken } | Out-Null
            $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($actual -ne $ExpectedHashes[$name]) { throw "Generic Package SHA-256 mismatch: $name" }
            $records.Add([pscustomobject]@{ name = $name; size = [int64](Get-Item -LiteralPath $path).Length; sha256 = $actual; url = $url })
        } finally {
            if (Test-Path -LiteralPath $path) { [System.IO.File]::Delete($path) }
        }
    }
    return @($records)
}

function Publish-GenericPackageSet {
    param($Context, [hashtable]$Files, [hashtable]$Hashes)
    if (-not $Context.PackageFirst -or $Context.PackageName -ne 'teamkit' -or $Context.PackageVersion -ne 'v0.1.5') { throw 'Generic Package publication is available only for v0.1.5' }
    if (-not $Files -or $Files.Count -ne $Context.ReleaseFiles.Count -or -not $Hashes -or $Hashes.Count -ne $Context.ReleaseFiles.Count) { throw 'exact six-file package input is required' }
    foreach ($name in $Context.ReleaseFiles) {
        $path = $Files[$name]
        if ([string]::IsNullOrWhiteSpace([string]$path) -or -not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "package input is missing: $name" }
        $item = Get-Item -LiteralPath $path
        if ($item.Length -gt $script:MaxPublicationArchiveCompressedBytes) { throw "package input exceeds bound: $name" }
        $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $Hashes[$name]) { throw "package input hash mismatch: $name" }
    }
    Assert-GenericPackageSetAbsent $Context
    for ($index = 0; $index -lt $Context.ReleaseFiles.Count; $index++) {
        $name = $Context.ReleaseFiles[$index]
        $bytes = [System.IO.File]::ReadAllBytes($Files[$name])
        $response = Invoke-GenericPackageRequest $Context 'PUT' $name $bytes
        if ([int]$response.StatusCode -ne 201) { throw "Generic Package upload is ambiguous status=$($response.StatusCode): $name" }
        $prefix = @($Context.ReleaseFiles[0..$index])
        Assert-ExactGenericPackageInventory $Context $prefix $Hashes | Out-Null
    }
    return Test-GenericPackageSet $Context $Hashes -InventoryVerified
}

function Get-FixedUploadApiUrl {
    param($Context, $Upload)
    $configured = Get-ReleaseObjectValue $Upload 'ApiUrl'
    if (-not [string]::IsNullOrWhiteSpace([string]$configured)) {
        $apiUri = [Uri]$configured
        if ($apiUri.Scheme -ne 'https') { throw 'fixed upload API URL must use HTTPS' }
        return $apiUri.AbsoluteUri
    }
    $browserUri = [Uri]$Upload.Url
    $baseUri = [Uri]$Context.GitLabBaseUrl
    if ($browserUri.Scheme -ne 'https' -or $browserUri.Host -ne $baseUri.Host) { throw 'fixed upload browser URL is not an approved GitLab origin' }
    if ($browserUri.AbsolutePath -notmatch '^/-/project/12087/uploads/([0-9a-f]{32})/([^/]+)$') { throw 'fixed upload browser URL cannot be converted to GitLab API path' }
    $secret = $Matches[1]
    $fileName = [Uri]::EscapeDataString($Matches[2])
    return "$($Context.GitLabBaseUrl)/api/v4/projects/$($Context.GitLabProjectId)/uploads/$secret/$fileName"
}

function Test-FixedUploadSet {
    param($Context)
    $hashes = @{}
    $records = [System.Collections.Generic.List[object]]::new()
    foreach ($upload in $Context.UploadFiles) {
        $path = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-upload-" + [Guid]::NewGuid().ToString('N'))
        try {
            $apiUrl = Get-FixedUploadApiUrl $Context $upload
            Save-ReleaseDownload $Context $apiUrl $path @{ 'PRIVATE-TOKEN' = $Context.GitLabToken } -AllowRedirect | Out-Null
            if ((Get-Item -LiteralPath $path).Length -ne $upload.Size -or (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $upload.Sha256) { throw "fixed upload verification failed: $($upload.Name)" }
            $hashes[$upload.Name] = $upload.Sha256
            $records.Add([pscustomobject]@{ name = $upload.Name; size = [int64]$upload.Size; sha256 = $upload.Sha256; url = $upload.Url })
        } finally {
            if (Test-Path -LiteralPath $path) { [System.IO.File]::Delete($path) }
        }
    }
    return @{ Hashes = $hashes; Files = @($records) }
}

function Test-ExactMaintainerTagRule {
    param($Rule, [string]$Version)
    if ($Rule.name -ne $Version) { return $false }
    $levels = @($Rule.create_access_levels)
    if ($levels.Count -ne 1) { return $false }
    $level = $levels[0]
    if ([int]$level.access_level -ne 40) { return $false }
    foreach ($forbidden in @('user_id', 'group_id', 'deploy_key_id')) {
        if ($null -ne $level[$forbidden]) { return $false }
    }
    return $true
}

function Test-ExactAnnotatedRemoteTag {
    param($Tag, $Context)
    if ($Tag.name -ne $Context.Version -or $Tag.commit.id -ne $Context.CandidateSha) { return $false }
    if ($Tag.target -notmatch '^[0-9a-f]{40}$' -or $Tag.target -eq $Context.CandidateSha) { return $false }
    if ($Tag.message -ne "1C Team Kit $($Context.Version)" -or [string]::IsNullOrWhiteSpace($Tag.created_at)) { return $false }
    return $true
}

function Get-ExactLocalAnnotatedTag {
    param($Context)
    $rawRefs = (Invoke-BoundedProcess $Context 'git' @('for-each-ref', '--format=%(objectname)', "refs/tags/$($Context.Version)")).StdOut -split "`r?`n"
    $refLines = @($rawRefs | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($refLines.Count -eq 0) { return $null }
    if ($refLines.Count -ne 1 -or $refLines[0].Trim() -notmatch '^[0-9a-f]{40}$') { throw 'local tag ref is malformed' }
    $tagObject = (Invoke-BoundedProcess $Context 'git' @('rev-parse', "$($Context.Version)^{tag}")).StdOut.Trim()
    $peeled = (Invoke-BoundedProcess $Context 'git' @('rev-parse', ($Context.Version + '^{}'))).StdOut.Trim()
    $tagType = (Invoke-BoundedProcess $Context 'git' @('cat-file', '-t', $tagObject)).StdOut.Trim()
    $message = (Invoke-BoundedProcess $Context 'git' @('for-each-ref', '--format=%(contents)', "refs/tags/$($Context.Version)")).StdOut.TrimEnd([char[]]@("`r", "`n"))
    if ($tagObject -ne $refLines[0].Trim() -or $tagType -ne 'tag' -or $peeled -ne $Context.CandidateSha -or $message -ne "1C Team Kit $($Context.Version)") { throw 'local annotated tag identity mismatch' }
    return @{ tag_object_sha = $tagObject; peeled_commit_sha = $peeled }
}

function Get-OptionalGitLabResource {
    param($Context, [string]$Path)
    try { return Invoke-GitLabApi $Context 'GET' $Path $null } catch {
        if ($_.Exception.Message -match 'status=404') { return $null }
        throw
    }
}

function New-ReleaseNotes {
    param($Context, $CI)
    $lines = [System.Collections.Generic.List[string]]::new()
    $lines.Add("Commit: $($Context.CandidateSha)")
    $lines.Add("GitHub run: $($CI.github_run_id)")
    $lines.Add("GitLab pipeline/job: $($CI.gitlab_pipeline_id)/$($CI.gitlab_job_id)")
    foreach ($name in $Context.ReleaseFiles) {
        $url = if ($Context.PackageFirst) { Get-GenericPackageUrl $Context $name } else { "$($Context.GitLabBaseUrl)/1c/aisuz/ai/-/jobs/$($CI.gitlab_job_id)/artifacts/raw/dist/$name" }
        $lines.Add("$name $($CI.Hashes[$name]) $url")
    }
    return $lines -join "`n"
}

function Test-ExactGitLabRelease {
    param($Release, $Context, $CI)
    if ($Release.name -ne "1C Team Kit $($Context.Version)" -or $Release.tag_name -ne $Context.Version -or $Release.commit.id -ne $Context.CandidateSha) { return $false }
    if ($Release._links.self -ne "$($Context.GitLabBaseUrl)/1c/aisuz/ai/-/releases/$($Context.Version)") { return $false }
    if ($Release.description -ne (New-ReleaseNotes $Context $CI)) { return $false }
    $links = @($Release.assets.links)
    if ($Context.PackageFirst) {
        if ($links.Count -ne $Context.ReleaseFiles.Count) { return $false }
        foreach ($name in $Context.ReleaseFiles) {
            $url = Get-GenericPackageUrl $Context $name
            $label = Get-ReleaseAssetLabel $name
            $matches = @($links | Where-Object { $_.name -eq $label -and $_.url -eq $url -and $_.link_type -eq 'other' })
            if ($matches.Count -ne 1) { return $false }
        }
    } else {
        if ($links.Count -ne $Context.UploadFiles.Count) { return $false }
        foreach ($upload in $Context.UploadFiles) {
            $matches = @($links | Where-Object { $_.name -eq $upload.Name -and $_.url -eq $upload.Url -and $_.link_type -eq 'other' })
            if ($matches.Count -ne 1) { return $false }
        }
    }
    return $true
}

function Get-ReleaseNotesCI {
    param($Release, $Context)
    $lines = @([string]$Release.description -split "`n")
    if ($lines.Count -ne (3 + $Context.ReleaseFiles.Count) -or $lines[0] -cne "Commit: $($Context.CandidateSha)" -or $lines[1] -notmatch '^GitHub run: ([1-9][0-9]*)$') { return $null }
    $githubRunId = $Matches[1]
    if ($lines[2] -notmatch '^GitLab pipeline/job: ([1-9][0-9]*)/([1-9][0-9]*)$') { return $null }
    $pipelineId = $Matches[1]; $jobId = $Matches[2]
    $hashes = @{}
    for ($index = 0; $index -lt $Context.ReleaseFiles.Count; $index++) {
        $name = $Context.ReleaseFiles[$index]
        $url = [regex]::Escape("$($Context.GitLabBaseUrl)/1c/aisuz/ai/-/jobs/$jobId/artifacts/raw/dist/$name")
        if ($lines[$index + 3] -notmatch ("^" + [regex]::Escape($name) + ' ([0-9a-f]{64}) ' + $url + '$')) { return $null }
        $hashes[$name] = $Matches[1]
    }
    return @{ github_run_id = $githubRunId; gitlab_pipeline_id = $pipelineId; gitlab_job_id = $jobId; Hashes = $hashes }
}

function Get-ReleaseAssetLabel {
    param([string]$Name)
    switch -Regex ($Name) {
        '^teamkit-v[0-9.]+-windows-amd64\.exe$' { return 'Windows amd64' }
        '^teamkit-v[0-9.]+-linux-amd64$' { return 'Linux amd64' }
        '^teamkit-v[0-9.]+-darwin-amd64$' { return 'macOS amd64' }
        '^teamkit-v[0-9.]+-darwin-arm64$' { return 'macOS arm64' }
        '^SHA256SUMS$' { return 'SHA256SUMS' }
        '^SECURITY-AUDIT\.json$' { return 'Отчёт аудита безопасности' }
        default { throw "missing Release asset label: $Name" }
    }
}

function Test-PreflightExactGitLabRelease {
    param($Release, $Context)
    if ($Release.name -ne "1C Team Kit $($Context.Version)" -or $Release.tag_name -ne $Context.Version -or $Release.commit.id -ne $Context.CandidateSha) { return $false }
    if ($Release._links.self -ne "$($Context.GitLabBaseUrl)/1c/aisuz/ai/-/releases/$($Context.Version)") { return $false }
    $links = @($Release.assets.links)
    if ($links.Count -ne $Context.UploadFiles.Count) { return $false }
    foreach ($upload in $Context.UploadFiles) {
        $matches = @($links | Where-Object { $_.name -eq $upload.Name -and $_.url -eq $upload.Url -and $_.link_type -eq 'other' })
        if ($matches.Count -ne 1) { return $false }
    }
    if ($null -eq (Get-ReleaseNotesCI $Release $Context)) { return $false }
    return $true
}

function Assert-ExistingReleasePublication {
    param($Context, $Release)
    $releaseCI = Get-ReleaseNotesCI $Release $Context
    if ($null -eq $releaseCI -or -not (Test-PreflightExactGitLabRelease $Release $Context)) { throw 'existing GitLab Release is not structurally exact' }
    $githubRun = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/runs/$($releaseCI.github_run_id)" $null
    if ([string]$githubRun.id -ne [string]$releaseCI.github_run_id -or $githubRun.path -ne '.github/workflows/ci.yml' -or $githubRun.head_sha -ne $Context.CandidateSha -or $githubRun.head_branch -ne $Context.GitHubBranch -or $githubRun.event -notin @('push', 'workflow_dispatch') -or $githubRun.conclusion -ne 'success') { throw 'existing Release GitHub run is not exact candidate evidence' }
    $pipeline = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/pipelines/$($releaseCI.gitlab_pipeline_id)" $null
    $pipelineSource = [string](Get-ReleaseObjectValue $pipeline 'source')
    if ([string]$pipeline.id -ne [string]$releaseCI.gitlab_pipeline_id -or $pipeline.sha -ne $Context.CandidateSha -or $pipeline.ref -ne $Context.GitLabBranch -or $pipelineSource -notin @('push', 'web', 'api') -or $pipeline.status -ne 'success') { throw 'existing Release GitLab pipeline is not exact candidate evidence' }
    $job = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/jobs/$($releaseCI.gitlab_job_id)" $null
    $jobPipelineId = Get-ReleaseObjectValue (Get-ReleaseObjectValue $job 'pipeline') 'id'
    if ([string]$job.id -ne [string]$releaseCI.gitlab_job_id -or $job.name -ne 'verify' -or $job.status -ne 'success' -or (Get-ReleaseObjectValue (Get-ReleaseObjectValue $job 'commit') 'id') -ne $Context.CandidateSha -or [string]$jobPipelineId -ne [string]$releaseCI.gitlab_pipeline_id -or $null -ne $job.artifacts_expire_at) { throw 'existing Release GitLab job is not exact candidate evidence' }
    $publication = Get-ProductionPublicationSets $Context $releaseCI -ExpectedHashes $releaseCI.Hashes
    foreach ($name in $Context.ReleaseFiles) { if ($publication.Hashes[$name] -ne $releaseCI.Hashes[$name]) { throw "existing Release hash does not match candidate evidence: $name" } }
    if (-not (Test-ExactGitLabRelease $Release $Context $releaseCI)) { throw 'existing GitLab Release does not match verified evidence' }
    $releaseCI.GitHubArtifactDigest = $publication.GitHubArtifactDigest
    return $releaseCI
}

function Assert-BranchFastForward {
    param($Context, [string]$RepositoryUrl, [string]$Branch)
    Invoke-BoundedProcess $Context 'git' @('fetch', '--no-tags', $RepositoryUrl, "refs/heads/$Branch") | Out-Null
    Invoke-BoundedProcess $Context 'git' @('merge-base', '--is-ancestor', 'FETCH_HEAD', $Context.CandidateSha) | Out-Null
}

function Test-LocalCandidateIdentity {
    param($Context)
    $buildDate = (Invoke-BoundedProcess $Context 'git' @('show', '-s', '--format=%cI', $Context.CandidateSha)).StdOut.Trim()
    $binary = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-preflight-" + [Guid]::NewGuid().ToString('N') + '.exe')
    $flags = "-X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.version=$($Context.Version) -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.commit=$($Context.CandidateSha) -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.buildDate=$buildDate"
    Invoke-BoundedProcess $Context 'go' @('build', '-ldflags', $flags, '-o', $binary, './cmd/teamkit') | Out-Null
    $identity = (Invoke-BoundedProcess $Context $binary @('--version')).StdOut | ConvertFrom-Json -AsHashtable
    if ($identity.version -ne $Context.Version -or $identity.commit -ne $Context.CandidateSha) { throw 'local candidate version or build metadata mismatch' }
}

function Test-SecurityAuditEvidence {
    param($Context, $Audit, [string]$FailureMessage = 'local security audit gate failed')
    $findings = @($Audit.findings | Where-Object { $null -ne $_ })
    if ($Audit.commit -ne $Context.CandidateSha -or $Audit.passed -ne $true -or $findings.Count -ne 0) { throw $FailureMessage }
}

function Invoke-LocalReleaseSecurityGates {
    param($Context)
    # Rebuild the active release set and audit the exact candidate immediately
    # before any remote mutation. Both child processes inherit the one context
    # deadline through Invoke-BoundedProcess.
    Invoke-BoundedProcess $Context 'pwsh' @('-NoLogo', '-NoProfile', '-File', 'scripts/build.ps1', '-Version', $Context.Version) | Out-Null
    $auditPath = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-local-audit-" + [Guid]::NewGuid().ToString('N') + '.json')
    Invoke-BoundedProcess $Context 'go' @('run', './cmd/teamkit-security-audit', '--repository', '.', '--path', 'dist', '--commit', $Context.CandidateSha, '--history-ref', $Context.CandidateSha, '--output', $auditPath) | Out-Null
    if (-not $Context.ProcessAdapter) {
        if (-not (Test-Path -LiteralPath $auditPath -PathType Leaf)) { throw 'local security audit evidence is unavailable' }
        $audit = Get-Content -LiteralPath $auditPath -Raw -Encoding UTF8 | ConvertFrom-Json -AsHashtable
        Test-SecurityAuditEvidence $Context $audit
    }
}

function Test-PublicationArchiveEvidence {
    param($Context, [hashtable]$Files, [hashtable]$Hashes)
    $manifest = Get-Content -LiteralPath $Files['SHA256SUMS'] -Encoding UTF8 | Where-Object { $_.Trim() }
    if ($manifest.Count -ne 4) { throw 'SHA256SUMS must have exactly four binary rows' }
    foreach ($binary in $Context.ReleaseFiles[0..3]) {
        $expected = "$($Hashes[$binary])  $binary"
        $row = @($manifest | Where-Object { $_ -ceq $expected })
        if ($row.Count -ne 1) { throw "checksum manifest mismatch: $binary" }
    }
    $audit = Get-Content -LiteralPath $Files['SECURITY-AUDIT.json'] -Raw -Encoding UTF8 | ConvertFrom-Json -AsHashtable
    Test-SecurityAuditEvidence $Context $audit 'security audit does not prove candidate'
}

function Test-PublicationExecutableIdentity {
    param($Context, [hashtable]$Files)
    $versionResult = Invoke-BoundedProcess $Context $Files[$Context.ReleaseFiles[0]] @('--version')
    $version = $versionResult.StdOut | ConvertFrom-Json -AsHashtable
    if ($version.version -ne $Context.Version -or $version.commit -ne $Context.CandidateSha) { throw 'embedded candidate identity mismatch' }
}

function Get-ProductionPublicationSets {
    param($Context, $CI, [switch]$Kept, [hashtable]$ExpectedHashes)
    $scratch = Join-Path ([System.IO.Path]::GetTempPath()) ("teamkit-release-" + [Guid]::NewGuid().ToString('N'))
    [void][System.IO.Directory]::CreateDirectory($scratch)
    $glArchive = Join-Path $scratch 'gitlab.zip'
    Save-ReleaseDownload $Context "$($Context.GitLabBaseUrl)/api/v4/projects/$($Context.GitLabProjectId)/jobs/$($CI.gitlab_job_id)/artifacts" $glArchive @{ 'PRIVATE-TOKEN' = $Context.GitLabToken } -AllowRedirect | Out-Null
    $gitlabFiles = Expand-PublicationArchive $Context $glArchive 'GitLab'
    $gitlabHashes = Get-PublicationHashMap $Context $gitlabFiles
    Test-PublicationArchiveEvidence $Context $gitlabFiles $gitlabHashes
    if ($Context.PackageFirst) {
        $handoffJobId = Get-ReleaseObjectValue $CI 'gitlab_handoff_job_id'
        if ([string]::IsNullOrWhiteSpace([string]$handoffJobId)) {
            Test-PublicationExecutableIdentity $Context $gitlabFiles
            return @{ Files = $gitlabFiles; Hashes = $gitlabHashes; GitHubArtifactDigest = $CI.GitHubArtifactDigest; github_run_id = $CI.github_run_id; github_artifact_id = (Get-ReleaseObjectValue $CI 'github_artifact_id'); gitlab_pipeline_id = $CI.gitlab_pipeline_id; gitlab_handoff_job_id = $null; gitlab_job_id = $CI.gitlab_job_id; Kept = [bool]$Kept }
        }
        $handoffArchive = Join-Path $scratch 'handoff.zip'
        Save-ReleaseDownload $Context "$($Context.GitLabBaseUrl)/api/v4/projects/$($Context.GitLabProjectId)/jobs/$handoffJobId/artifacts" $handoffArchive @{ 'PRIVATE-TOKEN' = $Context.GitLabToken } -AllowRedirect | Out-Null
        $handoff = Expand-HandoffPublicationArchive $Context $handoffArchive
        $handoffHashes = Get-PublicationHashMap $Context $handoff.Files
        Test-PublicationArchiveEvidence $Context $handoff.Files $handoffHashes
        foreach ($name in $Context.ReleaseFiles) { if ($handoffHashes[$name] -ne $gitlabHashes[$name]) { throw "GitLab handoff byte mismatch: $name" } }
        if ($ExpectedHashes) { foreach ($name in $Context.ReleaseFiles) { if ($gitlabHashes[$name] -ne $ExpectedHashes[$name]) { throw "publication hash does not match expected evidence: $name" } } }
        Test-PublicationExecutableIdentity $Context $gitlabFiles
        return @{ Files = $gitlabFiles; Hashes = $gitlabHashes; GitHubArtifactDigest = $handoff.Manifest.github_artifact_digest; github_run_id = $handoff.Manifest.github_run_id; github_artifact_id = $handoff.Manifest.github_artifact_id; gitlab_pipeline_id = $CI.gitlab_pipeline_id; gitlab_handoff_job_id = $handoffJobId; gitlab_job_id = $CI.gitlab_job_id; Kept = [bool]$Kept }
    }
    if ($Kept) {
        if ($CI.GitHubArtifactDigest -notmatch '^sha256:[0-9a-f]{64}$') { throw 'initial GitHub candidate artifact digest is unavailable' }
        return @{ Files = $gitlabFiles; Hashes = $gitlabHashes; GitHubArtifactDigest = $CI.GitHubArtifactDigest; github_run_id = $CI.github_run_id; github_artifact_id = (Get-ReleaseObjectValue $CI 'github_artifact_id'); gitlab_pipeline_id = $CI.gitlab_pipeline_id; gitlab_job_id = $CI.gitlab_job_id; Kept = $true }
    }
    if (-not $Context.PackageFirst) {
        $ghArtifacts = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/runs/$($CI.github_run_id)/artifacts?per_page=100" $null
        $matches = @($ghArtifacts.artifacts | Where-Object { -not $_.expired -and $_.name -eq 'candidate-binaries' })
        if ($matches.Count -ne 1) { throw 'exact GitHub candidate artifact or digest is unavailable' }
        $ghArtifact = $matches[0]
    }
    if ($ghArtifact.expired -ne $false -or $ghArtifact.name -ne 'candidate-binaries' -or $ghArtifact.digest -notmatch '^sha256:[0-9a-f]{64}$') { throw 'exact GitHub candidate artifact or digest is unavailable' }
    $ghArchive = Join-Path $scratch 'github.zip'
    Save-ReleaseDownload $Context $ghArtifact.archive_download_url $ghArchive @{ Authorization = "Bearer $($Context.GitHubToken)"; Accept = 'application/vnd.github+json' } -AllowRedirect | Out-Null
    $githubFiles = Expand-PublicationArchive $Context $ghArchive 'GitHub' -Layout root
    $githubHashes = Get-PublicationHashMap $Context $githubFiles
    foreach ($name in $Context.ReleaseFiles) { if ($gitlabHashes[$name] -ne $githubHashes[$name]) { throw "dual-CI byte mismatch: $name" } }
    if ($ExpectedHashes) {
        foreach ($name in $Context.ReleaseFiles) { if ($gitlabHashes[$name] -ne $ExpectedHashes[$name]) { throw "publication hash does not match expected evidence: $name" } }
    }
    Test-PublicationExecutableIdentity $Context $gitlabFiles
    return @{ Files = $gitlabFiles; Hashes = $gitlabHashes; GitHubArtifactDigest = $ghArtifact.digest; github_run_id = $CI.github_run_id; github_artifact_id = (Get-ReleaseObjectValue $ghArtifact 'id'); gitlab_pipeline_id = $CI.gitlab_pipeline_id; gitlab_job_id = $CI.gitlab_job_id; Kept = [bool]$Kept }
}

function Assert-PreTagReserve {
    param($Context, $Data, [switch]$ExactPackage)
    Assert-RemainingBudget $Context 1200 | Out-Null
    $githubRef = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/git/ref/heads/$($Context.GitHubBranch)" $null
    $gitlabBranch = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/branches/$($Context.GitLabBranch)" $null
    if ($githubRef.object.sha -ne $Context.CandidateSha -or $gitlabBranch.commit.id -ne $Context.CandidateSha) { throw 'branch ref changed before tag' }
    $job = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/jobs/$($Data.gitlab_job_id)" $null
    if ($Context.PackageFirst) {
        $jobPipelineId = Get-ReleaseObjectValue (Get-ReleaseObjectValue $job 'pipeline') 'id'
        if ([string]$job.id -ne [string]$Data.gitlab_job_id -or $job.name -ne 'verify' -or $job.status -ne 'success' -or (Get-ReleaseObjectValue (Get-ReleaseObjectValue $job 'commit') 'id') -ne $Context.CandidateSha -or [string]$jobPipelineId -ne [string]$Data.gitlab_pipeline_id -or $null -ne $job.artifacts_expire_at) { throw 'kept exact GitLab verify job changed before tag' }
    } elseif ($null -ne $job.artifacts_expire_at) { throw 'kept job expired before tag' }
    $rule = Get-OptionalGitLabResource $Context "/projects/$($Context.GitLabProjectId)/protected_tags/$($Context.Version)"
    if ($rule -and -not (Test-ExactMaintainerTagRule $rule $Context.Version)) { throw 'protected tag rule conflict before publication' }
    $tag = Get-OptionalGitLabResource $Context "/projects/$($Context.GitLabProjectId)/repository/tags/$($Context.Version)"
    if ($Context.PackageFirst -and $tag) { throw 'tag state appeared before first tag mutation' }
    if ($tag -and -not (Test-ExactAnnotatedRemoteTag $tag $Context)) { throw 'tag conflict before publication' }
    if ($tag) { $Context.State.ForwardOnly = $true }
    $release = Get-OptionalGitLabResource $Context "/projects/$($Context.GitLabProjectId)/releases/$($Context.Version)"
    if ($Context.PackageFirst -and $release) { throw 'Release state appeared before first tag mutation' }
    if ($release -and -not (Test-ExactGitLabRelease $release $Context $Data)) { throw 'Release conflict before publication' }
    if ($Context.PackageFirst) {
        if ($ExactPackage) { Assert-ExactGenericPackageInventory $Context @($Context.ReleaseFiles) $Data.Hashes | Out-Null } else { Assert-GenericPackageSetAbsent $Context }
    } else { Test-FixedUploadSet $Context | Out-Null }
    return @{ ok = $true }
}

function Invoke-ProductionReleaseStep {
    param($Context, [string]$Name, $Data)
    switch ($Name) {
        'preflight' {
            if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 is required' }
            foreach ($tool in @('git', 'go')) { if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is required" } }
            if ((Invoke-BoundedProcess $Context 'git' @('status', '--porcelain')).StdOut.Trim()) { throw 'working tree is not clean' }
            if ((Invoke-BoundedProcess $Context 'git' @('rev-parse', 'HEAD')).StdOut.Trim().ToLowerInvariant() -ne $Context.CandidateSha) { throw 'HEAD does not match candidate SHA' }
            Test-LocalCandidateIdentity $Context
            Invoke-BoundedProcess $Context 'go' @('test', './...') | Out-Null
            Invoke-BoundedProcess $Context 'go' @('test', './test/release') | Out-Null
            Invoke-BoundedProcess $Context 'go' @('vet', './...') | Out-Null
            Invoke-LocalReleaseSecurityGates $Context
            $authority = Assert-ReadOnlyApiAuthority $Context
            $githubRepository = $authority.GitHubRepository
            $githubRef = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/git/ref/heads/$($Context.GitHubBranch)" $null
            if ($githubRef.object.sha -notmatch '^[0-9a-f]{40}$') { throw 'GitHub branch ref is malformed' }
            $gitlabProject = $authority.GitLabProject
            $gitlabBranch = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/branches/$($Context.GitLabBranch)" $null
            if ($gitlabBranch.commit.id -notmatch '^[0-9a-f]{40}$') { throw 'GitLab branch ref is malformed' }
            if ($Context.PackageFirst) {
                if ($githubRef.object.sha -ne $Context.CandidateSha -or $gitlabBranch.commit.id -ne $Context.CandidateSha) { throw 'v0.1.5 release candidate must already be merged at both branch refs' }
            } else {
                Assert-BranchFastForward $Context 'https://github.com/mi1man-cmd/kit-all-team.git' $Context.GitHubBranch
                Assert-BranchFastForward $Context 'https://gitlab.example.invalid/1c/aisuz/ai.git' $Context.GitLabBranch
            }
            # A leftover local version tag can be pushed later by the recovery
            # path. Prove it now, before either remote credential probe or mutation.
            $Context.State.PreflightLocalTag = Get-ExactLocalAnnotatedTag $Context
            if (-not $Context.PackageFirst) {
                Invoke-BoundedProcess $Context 'git' @('-c', 'push.gpgSign=false', 'push', '--dry-run', 'https://github.com/mi1man-cmd/kit-all-team.git', "$($Context.CandidateSha):refs/heads/$($Context.GitHubBranch)") | Out-Null
                Invoke-BoundedProcess $Context 'git' @('-c', 'push.gpgSign=false', 'push', '--dry-run', 'https://gitlab.example.invalid/1c/aisuz/ai.git', "$($Context.CandidateSha):refs/heads/$($Context.GitLabBranch)") | Out-Null
            }
            $existingTag = Get-OptionalGitLabResource $Context "/projects/$($Context.GitLabProjectId)/repository/tags/$($Context.Version)"
            if ($Context.PackageFirst -and $existingTag) { throw 'existing v0.1.5 tag state is not publishable' }
            if ($existingTag -and -not (Test-ExactAnnotatedRemoteTag $existingTag $Context)) { throw 'conflicting existing remote tag' }
            if ($existingTag) { $Context.State.ForwardOnly = $true }
            $existingRule = Get-OptionalGitLabResource $Context "/projects/$($Context.GitLabProjectId)/protected_tags/$($Context.Version)"
            if ($existingRule -and -not (Test-ExactMaintainerTagRule $existingRule $Context.Version)) { throw 'conflicting existing protected tag rule' }
            $existingRelease = Get-OptionalGitLabResource $Context "/projects/$($Context.GitLabProjectId)/releases/$($Context.Version)"
            if ($Context.PackageFirst -and $existingRelease) { throw 'existing v0.1.5 Release state is not publishable' }
            if ($existingRelease -and -not (Test-PreflightExactGitLabRelease $existingRelease $Context)) { throw 'conflicting existing GitLab Release' }
            $existingReleaseCI = $null
            if ($existingRelease) {
                if (-not $existingTag -or -not $existingRule) { throw 'existing GitLab Release lacks exact protected annotated tag state' }
                if ($githubRef.object.sha -ne $Context.CandidateSha -or $gitlabBranch.commit.id -ne $Context.CandidateSha) { throw 'existing GitLab Release requires both branch refs at candidate' }
                $existingReleaseCI = Assert-ExistingReleasePublication $Context $existingRelease
            }
            if ($Context.PackageFirst) { Assert-GenericPackageSetAbsent $Context } else { Test-FixedUploadSet $Context | Out-Null }
            if ($existingReleaseCI) {
                return @{ ok = $true; ExistingReleaseCI = $existingReleaseCI; ExistingTag = @{ tag_object_sha = $existingTag.target; peeled_commit_sha = $existingTag.commit.id; confirmed = $true }; ExistingReleaseUrl = $existingRelease._links.self }
            }
            return @{ ok = $true }
        }
        'sync-refs' {
            if ($Context.PackageFirst) {
                $githubRef = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/git/ref/heads/$($Context.GitHubBranch)" $null
                $gitlabBranch = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/branches/$($Context.GitLabBranch)" $null
                if ($githubRef.object.sha -ne $Context.CandidateSha -or $gitlabBranch.commit.id -ne $Context.CandidateSha) { throw 'verify-only branch readback does not match candidate' }
                return @{ ok = $true; verify_only = $true }
            }
            $Context.State.RefSyncStartedAt = Get-ReleaseUtcNow $Context
            $githubCIBaseline = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/workflows/ci.yml/runs?head_sha=$($Context.CandidateSha)&per_page=20" $null
            $gitlabCIBaseline = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/pipelines?sha=$($Context.CandidateSha)&per_page=20" $null
            $Context.State.CIGitHubBaselineIds = @{}
            foreach ($run in @($githubCIBaseline.workflow_runs)) {
                $id = Get-ReleaseObjectValue $run 'id'
                if ($null -ne $id) { $Context.State.CIGitHubBaselineIds[[string]$id] = $true }
            }
            $Context.State.CIGitLabBaselineIds = @{}
            foreach ($pipeline in @($gitlabCIBaseline)) {
                $id = Get-ReleaseObjectValue $pipeline 'id'
                if ($null -ne $id) { $Context.State.CIGitLabBaselineIds[[string]$id] = $true }
            }
            $githubRef = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/git/ref/heads/$($Context.GitHubBranch)" $null
            $gitlabBranch = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/branches/$($Context.GitLabBranch)" $null
            $githubAtCandidate = $githubRef.object.sha -eq $Context.CandidateSha
            $gitlabAtCandidate = $gitlabBranch.commit.id -eq $Context.CandidateSha
            $Context.State.ExpectedGitHubCIEvent = if ($githubAtCandidate) { 'workflow_dispatch' } else { 'push' }
            $Context.State.ExpectedGitLabCISource = if ($gitlabAtCandidate) { 'api' } else { 'push' }
            $Context.State.ExpectedGitLabPipelineId = $null
            if ($githubAtCandidate) {
                Invoke-GitHubApi $Context 'POST' "/repos/$($Context.GitHubRepository)/actions/workflows/ci.yml/dispatches" @{ ref = $Context.GitHubBranch; inputs = @{ expected_sha = $Context.CandidateSha } } | Out-Null
            } else {
                Invoke-BoundedProcess $Context 'git' @('-c', 'push.gpgSign=false', 'push', 'https://github.com/mi1man-cmd/kit-all-team.git', "$($Context.CandidateSha):refs/heads/$($Context.GitHubBranch)") | Out-Null
            }
            $githubRef = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/git/ref/heads/$($Context.GitHubBranch)" $null
            if ($githubRef.object.sha -ne $Context.CandidateSha) { throw 'GitHub branch readback does not match candidate' }
            if ($gitlabAtCandidate) {
                $createdPipeline = Invoke-GitLabApi $Context 'POST' "/projects/$($Context.GitLabProjectId)/pipeline" @{ ref = $Context.GitLabBranch }
                if ([string]$createdPipeline.id -notmatch '^[1-9][0-9]*$' -or $createdPipeline.sha -ne $Context.CandidateSha -or $createdPipeline.ref -ne $Context.GitLabBranch) { throw 'GitLab API pipeline dispatch is not exact candidate evidence' }
                $Context.State.ExpectedGitLabPipelineId = [string]$createdPipeline.id
            } else {
                Invoke-BoundedProcess $Context 'git' @('-c', 'push.gpgSign=false', 'push', 'https://gitlab.example.invalid/1c/aisuz/ai.git', "$($Context.CandidateSha):refs/heads/$($Context.GitLabBranch)") | Out-Null
            }
            $gitlabBranch = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/branches/$($Context.GitLabBranch)" $null
            if ($gitlabBranch.commit.id -ne $Context.CandidateSha) { throw 'GitLab branch readback does not match candidate' }
            return @{ ok = $true }
        }
        'wait-ci' {
            if ($Context.PackageFirst) { return Get-VerifiedExactCI $Context }
            return Get-ExactPipeline $Context
        }
        'compare-six' {
            $publication = Get-ProductionPublicationSets $Context $Data
            $Context.State.InitialGitLabHashes = @{}
            foreach ($name in $publication.Hashes.Keys) { $Context.State.InitialGitLabHashes[$name] = $publication.Hashes[$name] }
            return $publication
        }
        'keep' {
            Invoke-GitLabApi $Context 'POST' "/projects/$($Context.GitLabProjectId)/jobs/$($Data.gitlab_job_id)/artifacts/keep" $null | Out-Null
            while ($true) {
                $job = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/jobs/$($Data.gitlab_job_id)" $null
                if ($null -eq $job.artifacts_expire_at) { return $Data }
                Invoke-ReleaseSleep $Context 10
            }
        }
        'reverify-kept' {
            $publication = Get-ProductionPublicationSets $Context $Data -Kept
            $initial = $Context.State.InitialGitLabHashes
            if (-not $initial -or $initial.Count -ne $Context.ReleaseFiles.Count) { throw 'initial GitLab artifact hashes are unavailable' }
            foreach ($name in $Context.ReleaseFiles) { if ($publication.Hashes[$name] -ne $initial[$name]) { throw "kept GitLab artifact changed: $name" } }
            Test-PublicationExecutableIdentity $Context $publication.Files
            return $publication
        }
        'final-validation' {
            if ($Context.PackageFirst) {
                if ($Data.Hashes.Count -ne $Context.ReleaseFiles.Count) { throw 'GitLab final validation hashes are incomplete' }
                foreach ($name in $Context.ReleaseFiles) {
                    if ($Data.Hashes[$name] -notmatch '^[0-9a-f]{64}$') { throw "GitLab final validation hash is invalid: $name" }
                }
                return $Data
            }
            $joinedHashes = ($Context.ReleaseFiles | ForEach-Object { '{0}  {1}' -f $Data.Hashes[$_], $_ }) -join "`n"
            if ($Data.GitHubArtifactDigest -notmatch '^sha256:[0-9a-f]{64}$') { throw 'candidate artifact digest is malformed' }
            $baseline = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/workflows/release.yml/runs?head_sha=$($Context.CandidateSha)&per_page=20" $null
            $baselineIds = @{}; foreach ($run in @($baseline.workflow_runs | Where-Object { $null -ne $_ })) { $baselineIds[[string]$run.id] = $true }
            $Context.State.FinalDispatchBaselineIds = $baselineIds
            $Context.State.FinalDispatchStartedAt = Get-ReleaseUtcNow $Context
            Invoke-GitHubApi $Context 'POST' "/repos/$($Context.GitHubRepository)/actions/workflows/release.yml/dispatches" @{ ref = $Context.GitHubBranch; inputs = @{ ci_run_id = [string]$Data.github_run_id; candidate_digest = $Data.GitHubArtifactDigest; gitlab_sha256s = $joinedHashes } } | Out-Null
            $until = Get-RemainingSeconds $Context
            while ($until -gt 0) {
                $runs = Invoke-GitHubApi $Context 'GET' "/repos/$($Context.GitHubRepository)/actions/workflows/release.yml/runs?head_sha=$($Context.CandidateSha)&per_page=20" $null
                $candidates = @($runs.workflow_runs | Where-Object {
                    $createdAt = ConvertTo-ReleaseUtcTimestamp $_.created_at
                    $_.path -eq '.github/workflows/release.yml' -and $_.head_sha -eq $Context.CandidateSha -and $_.head_branch -eq $Context.GitHubBranch -and $_.event -eq 'workflow_dispatch' -and $null -ne $createdAt -and $createdAt -ge $Context.State.FinalDispatchStartedAt -and -not $baselineIds.ContainsKey([string]$_.id)
                })
                if ($candidates.Count -gt 1) { throw 'ambiguous post-dispatch final validation runs' }
                if ($candidates.Count -eq 1) {
                    if ($candidates[0].conclusion -eq 'success') { return $Data }
                    if ($candidates[0].status -eq 'completed') { throw 'final validation failed' }
                }
                Invoke-ReleaseSleep $Context 10
                $until = Get-RemainingSeconds $Context
            }
            throw [System.TimeoutException]::new('final validation did not finish')
        }
        'reserve' {
            return Assert-PreTagReserve $Context $Data
        }
        'post-package-reserve' {
            if (-not $Context.PackageFirst) { throw 'post-package reserve is available only for v0.1.5' }
            return Assert-PreTagReserve $Context $Data -ExactPackage
        }
        'package' {
            $Data.PackageRecords = @(Publish-GenericPackageSet $Context $Data.Files $Data.Hashes)
            return $Data
        }
        'revalidate-authority' {
            # Scope, token lifetime, and effective Maintainer access can change
            # after preflight. Re-prove them immediately before each mutating
            # GitLab boundary using GET-only probes.
            Assert-ReadOnlyApiAuthority $Context | Out-Null
            return @{ ok = $true }
        }
        'protect-tag' {
            $rule = $null
            try { $rule = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/protected_tags/$($Context.Version)" $null } catch {
                if ($_.Exception.Message -notmatch 'status=404') { throw }
            }
            if (-not $rule) {
                Invoke-GitLabApi $Context 'POST' "/projects/$($Context.GitLabProjectId)/protected_tags" @{ name = $Context.Version; create_access_level = 40 } | Out-Null
                $rule = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/protected_tags/$($Context.Version)" $null
            }
            if (-not (Test-ExactMaintainerTagRule $rule $Context.Version)) { throw 'protected tag rule is not exact Maintainer-only contract' }
            return @{ ok = $true }
        }
        'tag' {
            $remoteTag = $null
            try { $remoteTag = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/tags/$($Context.Version)" $null } catch {
                if ($_.Exception.Message -notmatch 'status=404') { throw }
            }
            if ($remoteTag) {
                if (-not (Test-ExactAnnotatedRemoteTag $remoteTag $Context)) { throw 'conflicting remote annotated tag' }
                $Context.State.ForwardOnly = $true
                return @{ tag_object_sha = $remoteTag.target; peeled_commit_sha = $remoteTag.commit.id; confirmed = $true }
            }
            $localTag = Get-ExactLocalAnnotatedTag $Context
            if (-not $localTag) {
                Invoke-BoundedProcess $Context 'git' @('-c', 'tag.gpgSign=false', 'tag', '-a', $Context.Version, $Context.CandidateSha, '-m', "1C Team Kit $($Context.Version)") | Out-Null
                $localTag = Get-ExactLocalAnnotatedTag $Context
                if (-not $localTag) { throw 'local annotated tag identity mismatch' }
            }
            Invoke-BoundedProcess $Context 'git' @('-c', 'push.gpgSign=false', 'push', 'https://gitlab.example.invalid/1c/aisuz/ai.git', "refs/tags/$($Context.Version)") | Out-Null
            $remoteTag = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/tags/$($Context.Version)" $null
            if (-not (Test-ExactAnnotatedRemoteTag $remoteTag $Context) -or $remoteTag.target -ne $localTag.tag_object_sha -or $remoteTag.commit.id -ne $localTag.peeled_commit_sha) { throw 'remote annotated tag identity mismatch' }
            $Context.State.ForwardOnly = $true
            return @{ tag_object_sha = $localTag.tag_object_sha; peeled_commit_sha = $localTag.peeled_commit_sha; confirmed = $true }
        }
        'release' {
            $notes = New-ReleaseNotes $Context $Data.CI
            $links = if ($Context.PackageFirst) {
                @($Context.ReleaseFiles | ForEach-Object { @{ name = (Get-ReleaseAssetLabel $_); url = (Get-GenericPackageUrl $Context $_); link_type = 'other' } })
            } else {
                @($Context.UploadFiles | ForEach-Object { @{ name = $_.Name; url = $_.Url; link_type = 'other' } })
            }
            $release = $null
            try { $release = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/releases/$($Context.Version)" $null } catch {
                if ($_.Exception.Message -notmatch 'status=404') { throw }
            }
            if ($release -and -not (Test-ExactGitLabRelease $release $Context $Data.CI)) { throw 'conflicting GitLab Release' }
            if (-not $release) {
                Invoke-GitLabApi $Context 'POST' "/projects/$($Context.GitLabProjectId)/releases" @{ name = "1C Team Kit $($Context.Version)"; tag_name = $Context.Version; description = $notes; assets = @{ links = $links } } | Out-Null
                $release = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/releases/$($Context.Version)" $null
                if (-not (Test-ExactGitLabRelease $release $Context $Data.CI)) { throw 'created GitLab Release verification failed' }
            }
            return @{ url = $release._links.self }
        }
        'verify-eight' {
            $kept = Get-ProductionPublicationSets $Context $Data.CI -Kept
            foreach ($name in $Context.ReleaseFiles) {
                if ($kept.Hashes[$name] -ne $Data.CI.Hashes[$name]) { throw "post-verification kept GitLab artifact changed: $name" }
            }
            Test-PublicationExecutableIdentity $Context $kept.Files
            $rule = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/protected_tags/$($Context.Version)" $null
            if (-not (Test-ExactMaintainerTagRule $rule $Context.Version)) { throw 'published protected tag rule no longer matches Maintainer-only contract' }
            $remoteTag = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/repository/tags/$($Context.Version)" $null
            if (-not (Test-ExactAnnotatedRemoteTag $remoteTag $Context)) { throw 'published remote annotated tag no longer matches candidate' }
            if ($Data.Tag -and ($remoteTag.target -ne $Data.Tag.tag_object_sha -or $remoteTag.commit.id -ne $Data.Tag.peeled_commit_sha)) { throw 'published remote annotated tag identity changed' }
            $Context.State.ForwardOnly = $true
            $release = Invoke-GitLabApi $Context 'GET' "/projects/$($Context.GitLabProjectId)/releases/$($Context.Version)" $null
            if (-not (Test-ExactGitLabRelease $release $Context $Data.CI)) { throw 'published Release no longer matches candidate' }
            $records = [System.Collections.Generic.List[object]]::new()
            if ($Context.PackageFirst) {
                foreach ($record in @(Test-GenericPackageSet $Context $Data.CI.Hashes)) { $records.Add($record) }
            } else {
                foreach ($name in $Context.ReleaseFiles) {
                    $records.Add([pscustomobject]@{ name = $name; size = [int64](Get-Item -LiteralPath $kept.Files[$name]).Length; sha256 = $kept.Hashes[$name]; url = "$($Context.GitLabBaseUrl)/1c/aisuz/ai/-/jobs/$($Data.CI.gitlab_job_id)/artifacts/raw/dist/$name" })
                }
                $uploads = Test-FixedUploadSet $Context
                foreach ($record in $uploads.Files) { $records.Add($record) }
            }
            $expectedCount = if ($Context.PackageFirst) { 6 } else { 8 }
            if ($records.Count -ne $expectedCount -or @($records | Select-Object -ExpandProperty name -Unique).Count -ne $expectedCount) { throw 'post-verification did not produce the exact distinct file records' }
            return @{ files = @($records) }
        }
        default { throw "unknown production operation: $Name" }
    }
}

function Publish-TeamKitRelease {
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Context)
    $Context.State.CurrentStage = 'preflight'
    $Context.State.FailureReasonCode = $null
    if (-not $Context.State.ContainsKey('ForwardOnly')) { $Context.State.ForwardOnly = $false }
    try {
        $preflight = Invoke-ReleasePreflight $Context
        $existingReleaseCI = Get-ReleaseObjectValue $preflight 'ExistingReleaseCI'
        if ($existingReleaseCI) {
            $existingCI = $existingReleaseCI
            $existingTag = Get-ReleaseObjectValue $preflight 'ExistingTag'
            $existingRelease = @{ url = (Get-ReleaseObjectValue $preflight 'ExistingReleaseUrl') }
            $verification = Test-PublishedRelease $Context $existingCI $existingRelease $existingTag
            return @{ status = 'published'; exit_code = 0; version = $Context.Version; commit = $Context.CandidateSha; release_url = $existingRelease.url; tag_object_sha = $existingTag.tag_object_sha; peeled_commit_sha = $existingTag.peeled_commit_sha; github_run_id = $existingCI.github_run_id; gitlab_pipeline_id = $existingCI.gitlab_pipeline_id; gitlab_job_id = $existingCI.gitlab_job_id; files = $verification.files; duration_seconds = Get-ElapsedSeconds $Context }
        }
        Sync-ReleaseRefs $Context | Out-Null
        $ci = Wait-ExactShaCI $Context
        $publication = Compare-PublicationSets $Context $ci
        $kept = Invoke-ReleaseStep $Context 'keep' $publication
        $publication = Invoke-ReleaseStep $Context 'reverify-kept' $kept
        $publication = Invoke-ReleaseStep $Context 'final-validation' $publication
        Invoke-ReleaseStep $Context 'reserve' $publication | Out-Null
        Assert-RemainingBudget $Context 1200 | Out-Null
        Invoke-ReleaseStep $Context 'revalidate-authority' $publication | Out-Null
        if ($Context.PackageFirst) {
            $publication = Invoke-ReleaseStep $Context 'package' $publication
            Invoke-ReleaseStep $Context 'revalidate-authority' $publication | Out-Null
            Invoke-ReleaseStep $Context 'post-package-reserve' $publication | Out-Null
        }
        Assert-RemainingBudget $Context 1200 | Out-Null
        Invoke-ReleaseStep $Context 'protect-tag' $publication | Out-Null
        $tag = Invoke-ReleaseStep $Context 'tag' $publication
        Invoke-ReleaseStep $Context 'revalidate-authority' $publication | Out-Null
        $release = Publish-GitLabRelease $Context $publication $tag
        $verification = Test-PublishedRelease $Context $publication $release $tag
        return @{ status = 'published'; exit_code = 0; version = $Context.Version; commit = $Context.CandidateSha; release_url = $release.url; tag_object_sha = $tag.tag_object_sha; peeled_commit_sha = $tag.peeled_commit_sha; github_run_id = $ci.github_run_id; gitlab_pipeline_id = $ci.gitlab_pipeline_id; gitlab_job_id = $ci.gitlab_job_id; files = $verification.files; duration_seconds = Get-ElapsedSeconds $Context }
    } catch [System.TimeoutException] {
        $stage = if ([string]::IsNullOrWhiteSpace([string]$Context.State.CurrentStage)) { 'preflight' } else { [string]$Context.State.CurrentStage }
        return @{ status = 'deadline_exceeded'; exit_code = 124; version = $Context.Version; commit = $Context.CandidateSha; stage = $stage; reason_code = 'DEADLINE_EXCEEDED'; error = 'release deadline exceeded'; recovery = @{ forward_only = [bool]$Context.State.ForwardOnly }; duration_seconds = Get-ElapsedSeconds $Context }
    } catch {
        $stage = if ([string]::IsNullOrWhiteSpace([string]$Context.State.CurrentStage)) { 'preflight' } else { [string]$Context.State.CurrentStage }
        $reasonCode = if ([string]::IsNullOrWhiteSpace([string]$Context.State.FailureReasonCode)) { (($stage -replace '[^A-Za-z0-9]', '_').ToUpperInvariant() + '_FAILED') } else { [string]$Context.State.FailureReasonCode }
        return @{ status = 'failed'; exit_code = 1; version = $Context.Version; commit = $Context.CandidateSha; stage = $stage; reason_code = $reasonCode; error = 'release operation failed'; recovery = @{ forward_only = [bool]$Context.State.ForwardOnly }; duration_seconds = Get-ElapsedSeconds $Context }
    }
}

Export-ModuleMember -Function New-ReleaseContext,Get-RemainingSeconds,Assert-RemainingBudget,Invoke-BoundedProcess,Invoke-GitHubApi,Invoke-GitLabApi,Wait-ExactShaCI,Compare-PublicationSets,Invoke-ReleasePreflight,Sync-ReleaseRefs,Publish-ProtectedTag,Publish-GitLabRelease,Test-PublishedRelease,Publish-TeamKitRelease
