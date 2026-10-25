# Subtitle filesystem qualification v10 — 2026-10-25

Scope: the unmerged `qualification/stage-filesystem-v10` branch stacked on Server PR #20. This is a security **test candidate**, not approval to install the service or operate on Plex media.

## What passed (direct execution)

- Linux Go 1.27 `go test ./... -count=1`: all packages passed on synthetic fixtures. `go vet ./...` passed.
- Linux `internal/subtitles` negative tests reject same-size, restored-mtime edits outside the former first/middle/last 64 KiB sample windows. Manifest v2 now fingerprints the entire media file. Existing v1 manifests **must be restaged**; they are deliberately rejected.
- Linux mountinfo parser tests reject a bind alias of a library descendant, including escaped mount-point names. They are *parser fixtures*, not real bind mounts.
- Windows Lappy Go 1.27 `go test ./internal/subtitles -count=1`: passed.
- Windows Lappy, SMB 3.1.1 `\\TheSimsVault\TV Shows`, synthetic temporary test subdirectory: provider stage, review, no-overwrite hardlink publication, and idempotent retry passed for the current account. In-place same-size/mtime-restored modification during provider exchange was rejected and left no staged artifact. Directory relocation was attempted while `os.Root` was open; SMB denied the rename. Parallel local NTFS fixture likewise denied the rename.
- Each synthetic SMB test directory was removed after the run; no existing media was read or edited.

## What failed or remains unqualified

| Risk | Evidence / limitation | Disposition |
| --- | --- | --- |
| Staging confidentiality on Windows/SMB | Synthetic directory under the NAS TV Shows share inherited **11 allow/modify ACL entries**. Windows/SMB `0600` and `0700` are not a proof of private DACLs. No second-principal negative access test was available. `MEDIAPHILE_SUBTITLE_STAGE_ROOT` was not configured in the WorkBridge process. | **HOLD**. Provision a dedicated *local* private stage, inspect full effective DACL and share policy, then test denial as a separate untrusted account. Never stage private candidates into this inherited NAS share. |
| Bind-mount / namespace aliases | Linux parser tests passed, but the sandbox's effective Linux capabilities were zero: no real bind mount was created. Windows path/ancestor `os.SameFile` checks are partial and cannot prove all SMB/DFS alias cases. | **HOLD** for live namespace bind mount and Windows alternate-path tests. |
| Relocation after final check | Pinned `os.Root` and repeated checks reject observed swaps; live SMB and NTFS held the root against tested rename. A privileged actor, different filesystem, or a rename after final recheck is outside the demonstrated guarantee. | **HOLD** without a trusted, non-relocatable stage parent and attacker-capability definition. |
| Concurrent in-place media edits | Full-file identity and repeated stage checks reject deterministic edits during the provider exchange. They do **not** provide an atomic snapshot or deny a noncooperative writer changing bytes after the final hash but before the destination link. | **HOLD** for unfenced concurrent writers. Stop writers or use an immutable snapshot/exclusive storage lease, with real-platform verification. |
| Performance of full hashing | Full-file SHA-256 rechecks intentionally replace 64 KiB samples. Multi-GB NAS throughput and request timeout budgets have not been measured. | **HOLD** pending representative workload measurement. |

## Operational test procedure

1. Use an isolated, empty synthetic root on the actual target volume; **do not point tests at an episode directory**.
2. Execute `go test ./internal/subtitles -run '^TestSMBQualification' -v -count=1` on Windows with `MEDIAPHILE_QUALIFY_SMB_ROOT` set to the authorized writable share root. The tests create unique `.mediaphile-security-*` children and remove only those children.
3. From Windows PowerShell, inspect `Get-Acl` on the configured stage root and a newly created child. Confirm no inherited or explicit modify/read permissions for principals outside the intended service and administrator set. Verify a separate untrusted principal **cannot** read candidate or manifest, rename parent, create hardlinks, or publish a sidecar.
4. Under Linux, use an isolated mount namespace with authority to create a bind mount of a descendant of the media root to an otherwise unrelated stage path. Verify `stageOutsideDirectory` rejects it before provider I/O; rerun after mount namespace changes.
5. With actual NAS/Windows runtime, test a noncooperative writer that holds the media file open across the final hash/publication boundary. The current code cannot guarantee safety there: until an exclusive lease or immutable snapshot exists, do not mark this criterion passed.

No protected-main merge, live NAS deployment, permission change, or media mutation is authorized by these test results. Preserve the four security holds until independent exact-head environment evidence closes them.
