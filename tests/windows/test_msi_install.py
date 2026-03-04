"""
Integration tests for the Savecraft Windows MSI installer.

Runs on windows-latest GitHub Actions runners. Tests:
- MSI silent install puts binary in Program Files
- Startup registry key is created
- MSI silent uninstall cleans up
"""

import os
import subprocess
import sys
import winreg

INSTALL_DIR = os.path.join(os.environ.get("ProgramFiles", r"C:\Program Files"), "Savecraft")
DAEMON_PATH = os.path.join(INSTALL_DIR, "savecraftd.exe")
TRAY_PATH = os.path.join(INSTALL_DIR, "savecraft-tray.exe")
REGISTRY_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
DAEMON_REGISTRY_VALUE = "Savecraft Daemon"
TRAY_REGISTRY_VALUE = "Savecraft Tray"


def find_msi() -> str:
    """Find the MSI file in dist/."""
    dist = os.path.join(os.path.dirname(__file__), "..", "..", "dist")
    for f in os.listdir(dist):
        if f.endswith(".msi"):
            return os.path.join(dist, f)
    raise FileNotFoundError("No .msi file found in dist/")


def msi_install(msi_path: str) -> None:
    """Silently install the MSI."""
    result = subprocess.run(
        ["msiexec", "/i", msi_path, "/qn", "/norestart"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    if result.returncode != 0:
        print(f"STDOUT: {result.stdout}", file=sys.stderr)
        print(f"STDERR: {result.stderr}", file=sys.stderr)
        raise RuntimeError(f"MSI install failed with exit code {result.returncode}")


def msi_uninstall(msi_path: str) -> None:
    """Silently uninstall the MSI."""
    result = subprocess.run(
        ["msiexec", "/x", msi_path, "/qn", "/norestart"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    if result.returncode != 0:
        print(f"STDOUT: {result.stdout}", file=sys.stderr)
        print(f"STDERR: {result.stderr}", file=sys.stderr)
        raise RuntimeError(f"MSI uninstall failed with exit code {result.returncode}")


def test_install_creates_daemon():
    """After install, savecraftd.exe exists in Program Files."""
    assert os.path.isfile(DAEMON_PATH), f"Daemon not found at {DAEMON_PATH}"


def test_install_creates_tray():
    """After install, savecraft-tray.exe exists in Program Files."""
    assert os.path.isfile(TRAY_PATH), f"Tray not found at {TRAY_PATH}"


def test_install_creates_daemon_registry_key():
    """After install, HKCU Run key exists for Savecraft Daemon."""
    try:
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, REGISTRY_KEY, 0, winreg.KEY_READ)
        value, _ = winreg.QueryValueEx(key, DAEMON_REGISTRY_VALUE)
        winreg.CloseKey(key)
        assert "savecraftd.exe" in value, f"Registry value doesn't reference savecraftd.exe: {value}"
    except FileNotFoundError:
        raise AssertionError(f"Registry key {REGISTRY_KEY}\\{DAEMON_REGISTRY_VALUE} not found")


def test_install_creates_tray_registry_key():
    """After install, HKCU Run key exists for Savecraft Tray."""
    try:
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, REGISTRY_KEY, 0, winreg.KEY_READ)
        value, _ = winreg.QueryValueEx(key, TRAY_REGISTRY_VALUE)
        winreg.CloseKey(key)
        assert "savecraft-tray.exe" in value, f"Registry value doesn't reference savecraft-tray.exe: {value}"
    except FileNotFoundError:
        raise AssertionError(f"Registry key {REGISTRY_KEY}\\{TRAY_REGISTRY_VALUE} not found")


def test_daemon_runs():
    """The installed daemon starts and responds to version."""
    result = subprocess.run(
        [DAEMON_PATH, "version"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"Daemon exited with {result.returncode}: {result.stderr}"
    assert len(result.stdout.strip()) > 0, "No version output"


def test_uninstall_removes_daemon():
    """After uninstall, savecraftd.exe is removed."""
    assert not os.path.isfile(DAEMON_PATH), f"Daemon still exists at {DAEMON_PATH}"


def test_uninstall_removes_tray():
    """After uninstall, savecraft-tray.exe is removed."""
    assert not os.path.isfile(TRAY_PATH), f"Tray still exists at {TRAY_PATH}"


def test_uninstall_removes_daemon_registry_key():
    """After uninstall, HKCU Run key for daemon is removed."""
    try:
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, REGISTRY_KEY, 0, winreg.KEY_READ)
        winreg.QueryValueEx(key, DAEMON_REGISTRY_VALUE)
        winreg.CloseKey(key)
        raise AssertionError(f"Registry key {REGISTRY_KEY}\\{DAEMON_REGISTRY_VALUE} still exists after uninstall")
    except FileNotFoundError:
        pass  # Expected — key should not exist


def test_uninstall_removes_tray_registry_key():
    """After uninstall, HKCU Run key for tray is removed."""
    try:
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, REGISTRY_KEY, 0, winreg.KEY_READ)
        winreg.QueryValueEx(key, TRAY_REGISTRY_VALUE)
        winreg.CloseKey(key)
        raise AssertionError(f"Registry key {REGISTRY_KEY}\\{TRAY_REGISTRY_VALUE} still exists after uninstall")
    except FileNotFoundError:
        pass  # Expected — key should not exist


def main():
    msi_path = find_msi()
    print(f"Found MSI: {msi_path}")

    # Phase 1: Install
    print("Installing MSI...")
    msi_install(msi_path)

    failures = []

    print("Testing install...")
    for test in [
        test_install_creates_daemon,
        test_install_creates_tray,
        test_install_creates_daemon_registry_key,
        test_install_creates_tray_registry_key,
        test_daemon_runs,
    ]:
        try:
            test()
            print(f"  PASS: {test.__doc__.strip()}")
        except (AssertionError, Exception) as e:
            print(f"  FAIL: {test.__doc__.strip()}: {e}")
            failures.append(f"{test.__name__}: {e}")

    # Phase 2: Uninstall
    print("Uninstalling MSI...")
    msi_uninstall(msi_path)

    print("Testing uninstall...")
    for test in [
        test_uninstall_removes_daemon,
        test_uninstall_removes_tray,
        test_uninstall_removes_daemon_registry_key,
        test_uninstall_removes_tray_registry_key,
    ]:
        try:
            test()
            print(f"  PASS: {test.__doc__.strip()}")
        except (AssertionError, Exception) as e:
            print(f"  FAIL: {test.__doc__.strip()}: {e}")
            failures.append(f"{test.__name__}: {e}")

    if failures:
        print(f"\n{len(failures)} test(s) failed:")
        for f in failures:
            print(f"  - {f}")
        sys.exit(1)
    else:
        print(f"\nAll tests passed.")


if __name__ == "__main__":
    main()
