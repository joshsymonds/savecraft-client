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
	const daemonUrl = `${env.INSTALL_URL}/daemon/${env.APP_NAME}-daemon-windows-amd64.exe`;
	const trayUrl = `${env.INSTALL_URL}/daemon/${env.APP_NAME}-tray-windows-amd64.exe`;
	const statusPort = env.STATUS_PORT;

	// A .cmd file that embeds PowerShell — double-clickable on Windows.
	// Downloads binaries, strips Mark-of-the-Web, registers autostart,
	// starts daemon in background, launches tray, and prints the link code.
	const script = `@echo off
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference = 'Stop'; " ^
  "$dir = \\"$env:LOCALAPPDATA\\Savecraft\\"; " ^
  "Write-Host ''; " ^
  "Write-Host '  Savecraft Installer' -ForegroundColor Cyan; " ^
  "Write-Host '  ====================' -ForegroundColor Cyan; " ^
  "Write-Host ''; " ^
  "New-Item -ItemType Directory -Force -Path $dir | Out-Null; " ^
  "Write-Host '  [1/5] Downloading daemon...' -ForegroundColor White; " ^
  "Invoke-WebRequest -Uri '${daemonUrl}' -OutFile \\"$dir\\savecraft-daemon.exe\\"; " ^
  "Unblock-File -Path \\"$dir\\savecraft-daemon.exe\\"; " ^
  "Write-Host '  [2/5] Downloading tray...' -ForegroundColor White; " ^
  "Invoke-WebRequest -Uri '${trayUrl}' -OutFile \\"$dir\\savecraft-tray.exe\\"; " ^
  "Unblock-File -Path \\"$dir\\savecraft-tray.exe\\"; " ^
  "Write-Host '  [3/5] Registering autostart...' -ForegroundColor White; " ^
  "& \\"$dir\\savecraft-daemon.exe\\" install; " ^
  "Write-Host '  [4/5] Starting daemon...' -ForegroundColor White; " ^
  "& \\"$dir\\savecraft-daemon.exe\\" start; " ^
  "Write-Host '  [5/5] Launching tray...' -ForegroundColor White; " ^
  "Start-Process -FilePath \\"$dir\\savecraft-tray.exe\\"; " ^
  "Write-Host ''; " ^
  "Write-Host '  Waiting for registration...' -ForegroundColor Gray; " ^
  "$attempts = 0; " ^
  "$linkCode = $null; " ^
  "while ($attempts -lt 30 -and -not $linkCode) { " ^
  "  Start-Sleep -Seconds 1; " ^
  "  $attempts++; " ^
  "  try { " ^
  "    $resp = Invoke-RestMethod -Uri 'http://localhost:${statusPort}/link' -ErrorAction SilentlyContinue; " ^
  "    if ($resp.linkCode) { $linkCode = $resp.linkCode; $linkURL = $resp.linkURL } " ^
  "  } catch { } " ^
  "} " ^
  "Write-Host ''; " ^
  "if ($linkCode) { " ^
  "  Write-Host '  =============================' -ForegroundColor Green; " ^
  "  Write-Host \\"  Link code: $linkCode\\" -ForegroundColor Green; " ^
  "  Write-Host '  =============================' -ForegroundColor Green; " ^
  "  Write-Host ''; " ^
  "  Write-Host \\"  Visit $linkURL to connect this device.\\" -ForegroundColor Yellow; " ^
  "} else { " ^
  "  Write-Host '  Could not get link code. Check the tray icon.' -ForegroundColor Red; " ^
  "} " ^
  "Write-Host ''; " ^
  "Write-Host '  Installation complete. You can close this window.' -ForegroundColor Cyan; "
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
