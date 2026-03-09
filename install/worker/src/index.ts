interface Env {
	INSTALL_URL: string;
	SERVER_URL: string;
	REDIRECT_URL: string;
	APP_NAME: string;
	STATUS_PORT: string;
	INSTALL: R2Bucket;
}

interface DaemonManifest {
	version: string;
	ed25519PublicKey?: string;
	platforms: Record<string, unknown>;
}

interface InstallerMetadata {
	version: string;
}

const CLI_PATTERNS = ['curl', 'wget', 'httpie', 'powershell'];

function isCli(userAgent: string): boolean {
	const lower = userAgent.toLowerCase();
	return CLI_PATTERNS.some((p) => lower.includes(p));
}

function isWindows(userAgent: string): boolean {
	const lower = userAgent.toLowerCase();
	return lower.includes('windows') && !lower.includes('windows phone');
}

async function readJson<T>(bucket: R2Bucket, key: string): Promise<T | null> {
	const obj = await bucket.get(key);
	if (!obj) return null;
	return (await obj.json()) as T;
}

async function handleInstallScript(request: Request, env: Env): Promise<Response> {
	const ua = request.headers.get('user-agent') ?? '';

	if (!isCli(ua)) {
		if (isWindows(ua)) {
			return handleWindowsInstall(env);
		}
		return Response.redirect(env.REDIRECT_URL, 302);
	}

	const obj = await env.INSTALL.get('curl/install.sh');
	if (!obj) {
		return new Response('Install script not found\n', { status: 404 });
	}

	// Read metadata from R2 to inject into the script
	const [manifest, metadata] = await Promise.all([
		readJson<DaemonManifest>(env.INSTALL, 'daemon/manifest.json'),
		readJson<InstallerMetadata>(env.INSTALL, 'curl/metadata.json'),
	]);

	const pubkey = manifest?.ed25519PublicKey ?? '';
	const installerVersion = metadata?.version ?? 'dev';

	const script = await obj.text();
	const vars = [
		`SAVECRAFT_INSTALL_URL="${env.INSTALL_URL}"`,
		`SAVECRAFT_SERVER_URL="${env.SERVER_URL}"`,
		`SAVECRAFT_FRONTEND_URL="${env.REDIRECT_URL}"`,
		`SAVECRAFT_INSTALLER_VERSION="${installerVersion}"`,
		`SAVECRAFT_ED25519_PUBKEY="${pubkey}"`,
		`SAVECRAFT_APP_NAME="${env.APP_NAME}"`,
		`SAVECRAFT_STATUS_PORT="${env.STATUS_PORT}"`,
	].join('\n');
	const patched = `${vars}\n${script}`;

	return new Response(patched, {
		headers: {
			'content-type': 'text/x-shellscript; charset=utf-8',
			'content-disposition': 'inline; filename="install.sh"',
		},
	});
}

async function handleDaemon(path: string, env: Env): Promise<Response> {
	const key = `daemon/${path}`;
	const obj = await env.INSTALL.get(key);
	if (!obj) {
		return new Response('Not found\n', { status: 404 });
	}

	const contentType = path.endsWith('.json') ? 'application/json' : 'application/octet-stream';

	return new Response(obj.body, {
		headers: {
			'content-type': contentType,
			'content-length': obj.size.toString(),
		},
	});
}

function handleWindowsInstall(env: Env): Response {
	const appName = env.APP_NAME;
	const daemonUrl = `${env.INSTALL_URL}/daemon/${appName}-daemon-windows-amd64.exe`;
	const trayUrl = `${env.INSTALL_URL}/daemon/${appName}-tray-windows-amd64.exe`;
	const statusPort = env.STATUS_PORT;
	const daemonExe = `${appName}-daemon.exe`;
	const trayExe = `${appName}-tray.exe`;
	const trayProcess = `${appName}-tray`;

	// A .cmd file that embeds PowerShell — double-clickable on Windows.
	// The script is "dumb muscle": health check, kill, download, then hand off
	// to `savecraftd setup` which handles registration, autostart, and linking.
	const script = `@echo off
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference = 'Stop'; " ^
  "$dir = \\"$env:LOCALAPPDATA\\Savecraft\\"; " ^
  "$daemon = \\"$dir\\${daemonExe}\\"; " ^
  "$tray = \\"$dir\\${trayExe}\\"; " ^
  "Write-Host ''; " ^
  "Write-Host '  Savecraft Installer' -ForegroundColor Cyan; " ^
  "Write-Host '  ====================' -ForegroundColor Cyan; " ^
  "Write-Host ''; " ^
  "try { " ^
  "  $boot = Invoke-RestMethod -Uri 'http://localhost:${statusPort}/boot' -TimeoutSec 2; " ^
  "  if ($boot.state -eq 'running') { " ^
  "    $isLinked = $false; " ^
  "    try { Invoke-RestMethod -Uri 'http://localhost:${statusPort}/link' -TimeoutSec 2 | Out-Null } catch { $isLinked = $true }; " ^
  "    if ($isLinked) { " ^
  "      Write-Host '  Savecraft is already running and linked.' -ForegroundColor Green; " ^
  "      $a = Read-Host '  Reinstall? (Y/n)'; " ^
  "      if ($a -eq 'n' -or $a -eq 'N') { Write-Host '  No changes made.'; exit 0 } " ^
  "    } " ^
  "  } " ^
  "} catch { }; " ^
  "Write-Host '  Stopping existing processes...' -ForegroundColor Yellow; " ^
  "try { Invoke-RestMethod -Method POST -Uri 'http://localhost:${statusPort}/shutdown' -TimeoutSec 2 | Out-Null } catch { }; " ^
  "Start-Sleep -Seconds 2; " ^
  "try { taskkill /IM ${daemonExe} /F 2>$null | Out-Null } catch { }; " ^
  "Stop-Process -Name ${trayProcess} -Force -ErrorAction SilentlyContinue; " ^
  "Start-Sleep -Seconds 1; " ^
  "$portBusy = $false; " ^
  "try { $tcp = New-Object System.Net.Sockets.TcpClient; $tcp.Connect('localhost', ${statusPort}); $tcp.Close(); $portBusy = $true } catch { }; " ^
  "if ($portBusy) { " ^
  "  Write-Host '  ERROR: Port ${statusPort} is still in use by another program.' -ForegroundColor Red; " ^
  "  Write-Host '  Close the program using that port and try again.' -ForegroundColor Red; " ^
  "  exit 1 " ^
  "}; " ^
  "New-Item -ItemType Directory -Force -Path $dir | Out-Null; " ^
  "Write-Host '  [1/3] Downloading daemon...' -ForegroundColor White; " ^
  "try { Invoke-WebRequest -Uri '${daemonUrl}' -OutFile $daemon; Unblock-File -Path $daemon } catch { " ^
  "  Write-Host \\"  ERROR: Failed to download daemon: $_\\" -ForegroundColor Red; " ^
  "  exit 1 " ^
  "}; " ^
  "Write-Host '  [2/3] Downloading tray...' -ForegroundColor White; " ^
  "try { Invoke-WebRequest -Uri '${trayUrl}' -OutFile $tray; Unblock-File -Path $tray } catch { " ^
  "  Write-Host \\"  ERROR: Failed to download tray: $_\\" -ForegroundColor Red; " ^
  "  exit 1 " ^
  "}; " ^
  "if (-not (Test-Path $daemon) -or (Get-Item $daemon).Length -eq 0) { " ^
  "  Write-Host '  ERROR: Daemon binary is missing or empty after download.' -ForegroundColor Red; " ^
  "  exit 1 " ^
  "}; " ^
  "if (-not (Test-Path $tray) -or (Get-Item $tray).Length -eq 0) { " ^
  "  Write-Host '  ERROR: Tray binary is missing or empty after download.' -ForegroundColor Red; " ^
  "  exit 1 " ^
  "}; " ^
  "Write-Host '  [3/3] Setting up...' -ForegroundColor White; " ^
  "Write-Host ''; " ^
  "& $daemon setup; " ^
  "if ($LASTEXITCODE -ne 0) { " ^
  "  Write-Host ''; " ^
  "  Write-Host '  Setup failed. Check the errors above.' -ForegroundColor Red " ^
  "} else { " ^
  "  Write-Host ''; " ^
  "  Write-Host '  Installation complete.' -ForegroundColor Cyan " ^
  "} "
pause
`;

	return new Response(script, {
		headers: {
			'content-type': 'application/x-msdos-program',
			'content-disposition': `attachment; filename="savecraft-install.cmd"`,
		},
	});
}

export default {
	async fetch(request: Request, env: Env): Promise<Response> {
		const url = new URL(request.url);
		const path = url.pathname;

		if (path.startsWith('/daemon/')) {
			return handleDaemon(path.slice('/daemon/'.length), env);
		}

		return handleInstallScript(request, env);
	},
} satisfies ExportedHandler<Env>;
