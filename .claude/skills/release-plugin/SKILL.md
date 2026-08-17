---
name: release-plugin
description: Use when asked to "release the X plugin," cut, publish, or deploy a plugin, create a plugin tag, bump a plugin version, or rebuild the plugin manifest.
---

# Release Plugin

## Normal releases

Always release with `just release-plugin <game> <version>`. The recipe creates
and pushes one tag per push, watches its deployment, checks the production
aggregate, and prints the tag, commit SHA, and served version. Never use
`git push --tags` or push several tags together.

Always bump the version. Republishing the same version is not supported.
`min_daemon_version` in `plugin.toml` gates which daemons see the plugin; bump
it only when the daemon contract changed.

Never use `gh workflow run deploy-plugin.yml -f plugin=…` for production.

## Repairing the aggregate

If the served production aggregate is wrong, rebuild it from an existing
production plugin tag:

```sh
gh workflow run deploy-plugin.yml --ref <existing prod plugin tag> -f rebuild_only=true
```

For staging, use `--ref main`. Then verify production and run the canary:

```sh
curl -sS https://api.savecraft.gg/plugins/manifest.json | jq '.plugins|keys'
gh workflow run plugin-manifest-canary.yml
```
