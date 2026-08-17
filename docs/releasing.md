# Releases

Git tags are the version. Pushing a `v*` tag publishes Linux archives to GitHub
Releases through GoReleaser.

```sh
git tag -a v0.1.0 -m "codex-account v0.1.0"
git push origin v0.1.0
```

The tag must be SemVer (`v0.1.0`, `v1.2.3-rc.1`). The working tree on that
commit should be clean. CI and releases run on the org self-hosted Linux
runner. GoReleaser then builds `linux/amd64` and `linux/arm64`, writes
checksums, and creates the GitHub Release.

Local dry run (no tag, no publish):

```sh
make snapshot
```

`codex-account version` reports the tag on GoReleaser builds, the module
version on `go install …@vX.Y.Z`, and `devel-<sha>` for a local `go build`.
