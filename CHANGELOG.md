# Changelog

## [0.13.0](https://github.com/omniaura/agentflow/compare/v0.12.1...v0.13.0) (2026-10-24)


### ⚠ BREAKING CHANGES

* **afhttp:** afhttp.Error, Errorf, NotFound and Internal are gone; Unauthorized and SendError take a context; error bodies are errs JSON instead of {"error": "..."}.

### Features

* .agent sections generate the tool-calling loop ([#118](https://github.com/omniaura/agentflow/issues/118)) ([b4492f0](https://github.com/omniaura/agentflow/commit/b4492f0b43f9ebb885e90aa62a708d407b3fe890))
* .empty names what an empty query result means ([#129](https://github.com/omniaura/agentflow/issues/129)) ([b445ee1](https://github.com/omniaura/agentflow/commit/b445ee1248cc199c347f4c1cc4fe1803c4c6348c))
* .returns gives agents a typed, checked final answer ([#122](https://github.com/omniaura/agentflow/issues/122)) ([56c7d36](https://github.com/omniaura/agentflow/commit/56c7d36c886736e63bde2493e39cf3f7759ad072))
* .serve exposes an agent as an HTTP endpoint ([#124](https://github.com/omniaura/agentflow/issues/124)) ([bf3d0c7](https://github.com/omniaura/agentflow/commit/bf3d0c7ddb7ba351a542492131eb92462972529d))
* .wrap puts a route's result in an object ([#131](https://github.com/omniaura/agentflow/issues/131)) ([de1c781](https://github.com/omniaura/agentflow/commit/de1c7813f345b968689c2c23fe1a8fe53968ff13))
* af upgrade and the agentflow-migrate skill ([#123](https://github.com/omniaura/agentflow/issues/123)) ([fce78fd](https://github.com/omniaura/agentflow/commit/fce78fdb88dede55a45b02faba2cedbc0209af93))
* **afhttp:** report errors with go-kit errs ([#128](https://github.com/omniaura/agentflow/issues/128)) ([0ca3bea](https://github.com/omniaura/agentflow/commit/0ca3bea63ee8da5b3d43bd083f14a04933adce15))
* **agent:** answer on the last turn instead of failing at maxturns ([#136](https://github.com/omniaura/agentflow/issues/136)) ([f72ef37](https://github.com/omniaura/agentflow/commit/f72ef37dafd9a448dd48aa0327c1ac2e6108c345))
* **agent:** Check vets a .returns answer beyond its schema ([#133](https://github.com/omniaura/agentflow/issues/133)) ([67f24b8](https://github.com/omniaura/agentflow/commit/67f24b82126452ff2fe88edc7054b67640708673))
* Claude and OpenAI adapters for agents, ejectable as your gateway ([#119](https://github.com/omniaura/agentflow/issues/119)) ([ebf38f8](https://github.com/omniaura/agentflow/commit/ebf38f85034911bb90f5835ce8058780016388fe))
* **eject:** pop the runtime packages out into the repository, and upgrade them ([#113](https://github.com/omniaura/agentflow/issues/113)) ([9190ba4](https://github.com/omniaura/agentflow/commit/9190ba488d5942f9fbc51d7fe0e1ad7abc125457))
* **gen:** declare LLM tools with .tool sections ([#109](https://github.com/omniaura/agentflow/issues/109)) ([e46f420](https://github.com/omniaura/agentflow/commit/e46f420fcf25492b4a96fa21c519fb9d5e925ee4))
* **gen:** declare structured LLM outputs with .output sections ([#115](https://github.com/omniaura/agentflow/issues/115)) ([b4aebe1](https://github.com/omniaura/agentflow/commit/b4aebe15367d81e0747d62cbdd7a890f29774934))
* **gen:** serve sqlc queries as HTTP routes with .route sections ([#112](https://github.com/omniaura/agentflow/issues/112)) ([2d9123b](https://github.com/omniaura/agentflow/commit/2d9123b6cf82ea7eec5d226df8ee785ed7035639))
* **gen:** write a route's SQL in the .af file and hand it to sqlc ([#116](https://github.com/omniaura/agentflow/issues/116)) ([0131c3f](https://github.com/omniaura/agentflow/commit/0131c3f33ff3af894fca48554dae8bbba8bdaed1))
* link sqlc query files to tools with .sqlc ([#127](https://github.com/omniaura/agentflow/issues/127)) ([13c127c](https://github.com/omniaura/agentflow/commit/13c127cecc36710c8dc5a9c0bda94ea0bad6c0b0))
* **lint:** check .agent references live ([#120](https://github.com/omniaura/agentflow/issues/120)) ([0b61af4](https://github.com/omniaura/agentflow/commit/0b61af4c2b6469675826442365444d19af9f6102))
* **lsp:** lint diagnostics, formatting, definition, rename and section support ([#114](https://github.com/omniaura/agentflow/issues/114)) ([d0556c4](https://github.com/omniaura/agentflow/commit/d0556c41fabc12b3b6a1ff58304fa72ea57859b7)), closes [#92](https://github.com/omniaura/agentflow/issues/92)
* **route:** .map shapes a route's response with a Go func ([#135](https://github.com/omniaura/agentflow/issues/135)) ([fa36a5b](https://github.com/omniaura/agentflow/commit/fa36a5b2ed8b3bc51dcbaca57660c54b594964d2))
* **route:** Cache-Control for route results, no-store by default ([#134](https://github.com/omniaura/agentflow/issues/134)) ([f75c08d](https://github.com/omniaura/agentflow/commit/f75c08d98c5095c0a074a6f4d4eb8f8bbef184a0))
* **runtime:** afmodel wraps models with retries, fallback and metering ([#132](https://github.com/omniaura/agentflow/issues/132)) ([610752f](https://github.com/omniaura/agentflow/commit/610752f0e6eb206de60197ec14b7f683c6ba69a3))
* **skills:** ship an agent skill for writing .af files ([#111](https://github.com/omniaura/agentflow/issues/111)) ([87f9a63](https://github.com/omniaura/agentflow/commit/87f9a63175abfaacbfe18cd7d9a80847a6ba4102))
* stream .serve agent runs as server-sent events ([#126](https://github.com/omniaura/agentflow/issues/126)) ([1f83048](https://github.com/omniaura/agentflow/commit/1f8304890953c7d480d417b5dff0c457010a834d))
* **survey:** count hand-written tool-calling loops as .agent candidates ([#121](https://github.com/omniaura/agentflow/issues/121)) ([1821663](https://github.com/omniaura/agentflow/commit/1821663efb932f2cf8edbf9460eb738b3f8e1e18))
* **survey:** count read-check-write handlers ([#130](https://github.com/omniaura/agentflow/issues/130)) ([a62c561](https://github.com/omniaura/agentflow/commit/a62c561963b09fc8d4a2487292a19a729d2af20f))
* **survey:** estimate how much of a Go module could be defined in .af ([#117](https://github.com/omniaura/agentflow/issues/117)) ([026f194](https://github.com/omniaura/agentflow/commit/026f1948b93550019c01768e4a68155416bc5850))
* tools written in SQL, implemented by generated code ([#125](https://github.com/omniaura/agentflow/issues/125)) ([ff6f493](https://github.com/omniaura/agentflow/commit/ff6f4935fe0788a0ec92a07fe202db902e545387))

## [0.12.1](https://github.com/omniaura/agentflow/compare/v0.12.0...v0.12.1) (2026-10-23)


### Bug Fixes

* **gen:** name the generated package after the directory's package clause ([#107](https://github.com/omniaura/agentflow/issues/107)) ([f3d3b6f](https://github.com/omniaura/agentflow/commit/f3d3b6f14966a75c3c6666608c28bdd4339a2537))

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
