# Changelog

## [0.5.3](https://github.com/omniaura/agentflow/compare/v0.5.2...v0.5.3) (2026-03-26)


### Bug Fixes

* **ci:** address CodeRabbit review comments on release automation ([0b53688](https://github.com/omniaura/agentflow/commit/0b536887c1ee8bdd467c50e63a036a34564e0a23))
* **ci:** update goreleaser config for v2.14, regenerate examples ([fc2e85a](https://github.com/omniaura/agentflow/commit/fc2e85a8fc895463945c9f11ff2e02e85500ab5f))
* harden conditional operand generation ([#58](https://github.com/omniaura/agentflow/issues/58)) ([2d188ff](https://github.com/omniaura/agentflow/commit/2d188ff74f02006104522cb70de3cbd5904390ae))
* harden lsp parsing and document editor workflows ([#53](https://github.com/omniaura/agentflow/issues/53)) ([0f454fc](https://github.com/omniaura/agentflow/commit/0f454fc1a58eba2ba43e1d8fd88aa72ed72647ff))
* sanitize .af file operands to prevent code injection in generated Go ([#46](https://github.com/omniaura/agentflow/issues/46)) ([711caa7](https://github.com/omniaura/agentflow/commit/711caa7976986c88c785c98e850ec52b29501a02))
* use $GOPACKAGE fallback when .af files are in root scan directory ([#44](https://github.com/omniaura/agentflow/issues/44)) ([3a61cc8](https://github.com/omniaura/agentflow/commit/3a61cc8da541de959a35e77ac600aaafb20f4e01))
