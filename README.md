<h1 align="center">Rclone</h1>

<p align="center">
  <strong>Copy or move completed downloads to allowlisted rclone remotes.</strong>
</p>

<p align="center">
  <a href="https://github.com/krate-apps/fleet"><img src="https://img.shields.io/badge/Fleet-official%20extension-0B1220?logo=go&logoColor=white" alt="Fleet official extension" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" /></a>
  <img src="https://img.shields.io/badge/version-1.0.0-informational" alt="Version 1.0.0" />
  <img src="https://img.shields.io/badge/engine-rTorrent-brightgreen" alt="rTorrent" />
</p>

<p align="center">
  <a href="#configuration">Configuration</a> ·
  <a href="#install">Install</a> ·
  <a href="https://github.com/krate-apps/fleet">Fleet</a> ·
  <a href="https://github.com/krate-apps/fleet-official-bucket">Official bucket</a> ·
  <a href="https://github.com/krate-apps/fleet-official-bucket/blob/main/extensions.md">All extensions</a> ·
  <a href="https://github.com/krate-apps/fleet/blob/main/docs/developer/create-extension.md">Create an extension</a>

</p>

---

<p align="center"><code>org.fleet.rclone</code> · official Fleet extension · SDK worker package</p>


<!-- fleet-config:start -->
## Configuration

Turn this extension **on/off** with **Extensions → Activate / Deactivate**. There is no separate Enabled checkbox in settings; Apply only saves options. After a Fleet host upgrade, use **Reload workers** if RPC returns `extension worker not active`.

### Parameters

#### Rclone — remotes and rules

Copy finished torrents to an rclone remote. Build transfer rules visually (or Expert).

| Parameter | Type | Required | Notes |
| --- | --- | --- | --- |
| Poll interval (seconds) (`intervalSec`) | number | yes | How often the worker wakes up. Placeholder: `60` Advanced (under More options). |
| rclone binary override (empty = tools.rclone / PATH) (`rcloneBin`) | text | no | — |
| Resolved rclone path (hint after save) (`rcloneResolved`) | text | no | Advanced (under More options). |
| Allowlisted remotes (`remotesAllowText`) | textarea | yes | One remote name per line. Advanced (under More options). |
| Transfer rules (key=value — Expert) (`rulesText`) | textarea | yes | Also editable with the visual builder below. action=copy\|move remote=NAME dest={label}/{name} [label=…] Advanced (under More options). |
| After copy: rclone check then delete local (default off) (`deleteLocalAfterVerify`) | boolean | no | Advanced (under More options). |

#### Cancel transfer

Adjust Cancel transfer — essentials first, extras under More options.

| Parameter | Type | Required | Notes |
| --- | --- | --- | --- |
| Job id (`cancelJobId`) | text | yes | From List running transfers. |

### Actions

| Action | Notes |
| --- | --- |
| Scan completions now | Primary; Target: `global` |
| List running transfers | Target: `global` |
| Show journal | Target: `global` |
| Clear journal | Destructive; Target: `global` |

<!-- fleet-config:end -->

## Install

Requires a running [Fleet](https://github.com/krate-apps/fleet) host (rTorrent via SCGI).

### From the Official bucket

1. Enable the **Fleet Official** bucket (`org.fleet.official`) if it is not already on.
2. Open **Extensions** → find **`org.fleet.rclone`** → Install.
3. Configure from the extension’s page / Configure UI.

Listings live in [`fleet-official-bucket`](https://github.com/krate-apps/fleet-official-bucket) (`bucket/org.fleet.rclone.json`).  
`package.url` / `sha256` are filled when a GitHub Release asset is published.

### Local development

```bash
make build
```

Place the built worker under Fleet’s `extensions-src/` (or install from the Official bucket).

## Package layout

```text
fleet-ext-rclone/
  fleet.ext.yaml          # manifest (id, capabilities, frontend/backend)
  ui/extension.json       # declarative UI
  bin/*-worker            # JSON-RPC worker (stdin/stdout), when present
  cmd/                    # Go worker sources, when present
  LICENSE
```

## Related

| | |
| --- | --- |
| Host | [krate-apps/fleet](https://github.com/krate-apps/fleet) |
| Docs | [Create an extension](https://github.com/krate-apps/fleet/blob/main/docs/developer/create-extension.md) |
| All extensions | [Catalogue](https://github.com/krate-apps/fleet-official-bucket/blob/main/extensions.md) |
| Bucket listing | [fleet-official-bucket/bucket/org.fleet.rclone.json](https://github.com/krate-apps/fleet-official-bucket/blob/main/bucket/org.fleet.rclone.json) |
| Manifest API | `>=1.0.0 <2.0.0` |

---

<p align="center">
  Developed by the <a href="https://runkrate.com">Krate</a> team.<br />
  Released under the MIT License.
</p>

## Pack / release

```bash
make pack   # dist/<id>-linux-amd64.tar.gz + dist/SHA256SUMS
```

Tag `v*` to run `.github/workflows/release.yml` (uploads archive + `SHA256SUMS`).
