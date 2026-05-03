# Changelog

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
