# Releasing

## One-time setup

1. GitHub repo `alexmchughdev/terraform-provider-terradash` (the Terraform Registry requires the `terraform-provider-<name>` name). Public.
2. Signing key (RSA; the registry rejects ed25519):
   ```sh
   gpg --quick-gen-key "terradash releases <you@example.com>" rsa4096 sign 3y
   gpg --armor --export-secret-keys <FINGERPRINT>   # -> secret GPG_PRIVATE_KEY
   gpg --armor --export <FINGERPRINT>               # -> registry + key.gpg
   ```
3. Repo secrets: `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE` (empty if none).
4. Settings → Pages: deploy from branch `gh-pages`, `/` (after the first release creates it).
5. [registry.terraform.io](https://registry.terraform.io) → Publish → Provider: add the public key under your namespace, select the repo.
6. Optional: OpenTofu Registry — open a "Submit new provider" issue at [opentofu/registry](https://github.com/opentofu/registry/issues/new/choose) with the repo and public key.

## Each release

1. Move `Unreleased` in `CHANGELOG.md` to the new version; commit.
2. `git tag -a vX.Y.Z -m "terradash vX.Y.Z" && git push origin master vX.Y.Z`
3. The `Release` workflow:
   - GoReleaser: provider zips, `SHA256SUMS` + `.sig`, registry manifest, CLI archives, deb/rpm/apk/Arch packages, GitHub release.
   - `apt` job: adds the `.deb`s to the signed apt repository on `gh-pages`.
4. The Terraform Registry picks up the release automatically (webhook). Check the provider page.

## Verify

- `apt update && apt install terradash` on Debian/Ubuntu (see README).
- `terraform init` with `source = "alexmchughdev/terradash"`, `version = "X.Y.Z"`.
- `gpg --verify terraform-provider-terradash_X.Y.Z_SHA256SUMS.sig terraform-provider-terradash_X.Y.Z_SHA256SUMS` (provider zips and manifest only)
- `gpg --verify terradash_X.Y.Z_SHA256SUMS.sig terradash_X.Y.Z_SHA256SUMS` (packages, CLI binaries)

## Local dry run

```sh
make test lint
GRAFANA_URL=http://localhost:3000 GRAFANA_AUTH=admin:admin make testacc e2e
goreleaser release --snapshot --clean --skip=publish,sign
```
