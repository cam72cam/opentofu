Testing the release process:
* Generate GPG Key:
  - `gpg --full-generate-key`
  - `gpg --list-secret-keys --keyid-format=long`
  - `gpg --armor --export-secret-keys <ID>`
  - Add gpg environment in github project settings
  - Paste full output of export command into `GPG_PRIVATE_KEY`
* Add go-release pro key as `GORELEASER_KEY`
* Login to ubuntu-one account (snapcraft)
  - `snapcraft export-login snapfile`
  - `snapcraft register <username>-opentofu`
