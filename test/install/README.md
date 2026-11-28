# Installer tests

What `install.sh` and `install.ps1` do, as opposed to whether they parse.

| File | What it checks | How it runs |
| --- | --- | --- |
| `install-check.sh` | `install.sh` on a host that already has the binary: the join command's `--mode`, `--controller` and `--join-token` still reach the handoff. A stub `zoomies` stands in for the existing install, so it needs no network and writes only to a temporary directory. | `TEST_SH=dash test/install/install-check.sh install.sh`, as the `installer` job in `ci.yml` does. |
| `install.Tests.ps1` | `install.ps1`'s functions under Pester 5, with the script dot-sourced as a library: what must hold before the first download, that a download is held to the release signature, and what keeps the join token out of sight. | `Invoke-Pester -Path test/install/install.Tests.ps1 -CI`, in the Windows job. |
| `windows-smoke.ps1` | `install.ps1` end to end against a real controller built from this commit: the service is registered and started, a second run upgrades without enrolling again, and `-Uninstall` removes it. | `./test/install/windows-smoke.ps1 -Binary ./zoomies.exe`, in the Windows job. |
| `PSScriptAnalyzerSettings.psd1` | The analyser rules the PowerShell files are held to. | Read by the Windows job's lint step. |

`windows-smoke.ps1` installs a service and edits the machine PATH, so it refuses
to run anywhere that is not CI unless you pass `-IKnowThisChangesThisMachine`.
Run it in a throwaway VM.

`make test-upgrade` is the neighbouring tier: it runs the last published release
and upgrades it in place (see [`../upgrade`](../upgrade/README.md)).
