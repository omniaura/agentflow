# Changelog

## [0.12.0](https://github.com/omniaura/agentflow/compare/v0.11.0...v0.12.0) (2026-10-23)


### Features

* **check:** add af check to verify generated code, formatting and lint ([#104](https://github.com/omniaura/agentflow/issues/104)) ([a4f1917](https://github.com/omniaura/agentflow/commit/a4f1917f7844c6a26b5288885b04075b715d0dce)), closes [#87](https://github.com/omniaura/agentflow/issues/87)


### Bug Fixes

* **gen:** ignore whitespace around prompt bodies so af fmt never changes output ([#102](https://github.com/omniaura/agentflow/issues/102)) ([0e38451](https://github.com/omniaura/agentflow/commit/0e3845194828d66b3c6638b5b0f37fd4e4eabe6e))
* **release:** migrate cask post-install hook to install_steps ([#106](https://github.com/omniaura/agentflow/issues/106)) ([b77ce91](https://github.com/omniaura/agentflow/commit/b77ce916850b7d4a895662947b6bd87ce06f9733))

## [0.11.0](https://github.com/omniaura/agentflow/compare/v0.10.0...v0.11.0) (2026-10-21)


### Features

* generate typed slice loops with lexical aliases ([#83](https://github.com/omniaura/agentflow/issues/83)) ([fd7713a](https://github.com/omniaura/agentflow/commit/fd7713a0340ef6db5e6289401a8052b46dabb549))


### Bug Fixes

* **gen:** make af gen atomic so a failed generation never truncates output ([#99](https://github.com/omniaura/agentflow/issues/99)) ([7e6daec](https://github.com/omniaura/agentflow/commit/7e6daec35cd556eb23d910c2cd39264f825c0678)), closes [#86](https://github.com/omniaura/agentflow/issues/86)
* **release:** repair release automation and version stamping ([#100](https://github.com/omniaura/agentflow/issues/100)) ([1450397](https://github.com/omniaura/agentflow/commit/1450397360712ed9c6e8dfefd0166d655f40483a)), closes [#85](https://github.com/omniaura/agentflow/issues/85)

## [0.10.0](https://github.com/omniaura/agentflow/compare/v0.9.0...v0.10.0) (2026-05-03)


### Features

* **lint:** add af lint command ([#77](https://github.com/omniaura/agentflow/issues/77)) ([2e5b6e1](https://github.com/omniaura/agentflow/commit/2e5b6e1afac8c7f614ed6d341f131c40f350faf1)), closes [#75](https://github.com/omniaura/agentflow/issues/75)

## [0.9.0](https://github.com/omniaura/agentflow/compare/v0.8.1...v0.9.0) (2026-05-03)


### Features

* **fmt:** canonical formatter for .af files ([#73](https://github.com/omniaura/agentflow/issues/73)) ([745186a](https://github.com/omniaura/agentflow/commit/745186a47f77c4427517523f854a409395063429)), closes [#72](https://github.com/omniaura/agentflow/issues/72)

## [0.8.1](https://github.com/omniaura/agentflow/compare/v0.8.0...v0.8.1) (2026-05-03)


### Bug Fixes

* **ci:** repair Go Coverage workflow (closes [#67](https://github.com/omniaura/agentflow/issues/67)) ([#70](https://github.com/omniaura/agentflow/issues/70)) ([196ae64](https://github.com/omniaura/agentflow/commit/196ae640b74355288d2efc9ae03d307a4a06d635))

## [0.8.0](https://github.com/omniaura/agentflow/compare/v0.7.0...v0.8.0) (2026-05-03)


### Features

* add demo init command ([#68](https://github.com/omniaura/agentflow/issues/68)) ([32c5322](https://github.com/omniaura/agentflow/commit/32c5322819ce036d21ba2c41832e0b60439ad1be))

## [0.7.0](https://github.com/omniaura/agentflow/compare/v0.6.0...v0.7.0) (2026-05-03)


### Features

* add interactive prompt selection ([#64](https://github.com/omniaura/agentflow/issues/64)) ([3657295](https://github.com/omniaura/agentflow/commit/3657295c8554db74549e425273a1d56116c11fde))

## [0.6.0](https://github.com/omniaura/agentflow/compare/v0.5.3...v0.6.0) (2026-03-26)


### Features

* add Viper config file support for CLI flags ([#62](https://github.com/omniaura/agentflow/issues/62)) ([1cffa09](https://github.com/omniaura/agentflow/commit/1cffa098d3de87b93d4c3b9c720fa0bcc0da9930))


### Bug Fixes

* **ci:** address CodeRabbit review comments on release automation ([0b53688](https://github.com/omniaura/agentflow/commit/0b536887c1ee8bdd467c50e63a036a34564e0a23))
* **ci:** update goreleaser config for v2.14, regenerate examples ([fc2e85a](https://github.com/omniaura/agentflow/commit/fc2e85a8fc895463945c9f11ff2e02e85500ab5f))
* harden conditional operand generation ([#58](https://github.com/omniaura/agentflow/issues/58)) ([2d188ff](https://github.com/omniaura/agentflow/commit/2d188ff74f02006104522cb70de3cbd5904390ae))
* harden lsp parsing and document editor workflows ([#53](https://github.com/omniaura/agentflow/issues/53)) ([0f454fc](https://github.com/omniaura/agentflow/commit/0f454fc1a58eba2ba43e1d8fd88aa72ed72647ff))
* sanitize .af file operands to prevent code injection in generated Go ([#46](https://github.com/omniaura/agentflow/issues/46)) ([711caa7](https://github.com/omniaura/agentflow/commit/711caa7976986c88c785c98e850ec52b29501a02))

## [0.5.3](https://github.com/omniaura/agentflow/compare/v0.5.2...v0.5.3) (2026-03-26)


### Bug Fixes

* **ci:** address CodeRabbit review comments on release automation ([0b53688](https://github.com/omniaura/agentflow/commit/0b536887c1ee8bdd467c50e63a036a34564e0a23))
* **ci:** update goreleaser config for v2.14, regenerate examples ([fc2e85a](https://github.com/omniaura/agentflow/commit/fc2e85a8fc895463945c9f11ff2e02e85500ab5f))
* harden conditional operand generation ([#58](https://github.com/omniaura/agentflow/issues/58)) ([2d188ff](https://github.com/omniaura/agentflow/commit/2d188ff74f02006104522cb70de3cbd5904390ae))
* harden lsp parsing and document editor workflows ([#53](https://github.com/omniaura/agentflow/issues/53)) ([0f454fc](https://github.com/omniaura/agentflow/commit/0f454fc1a58eba2ba43e1d8fd88aa72ed72647ff))
* sanitize .af file operands to prevent code injection in generated Go ([#46](https://github.com/omniaura/agentflow/issues/46)) ([711caa7](https://github.com/omniaura/agentflow/commit/711caa7976986c88c785c98e850ec52b29501a02))
* use $GOPACKAGE fallback when .af files are in root scan directory ([#44](https://github.com/omniaura/agentflow/issues/44)) ([3a61cc8](https://github.com/omniaura/agentflow/commit/3a61cc8da541de959a35e77ac600aaafb20f4e01))
