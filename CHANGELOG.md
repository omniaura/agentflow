# Changelog

## [0.14.0](https://github.com/omniaura/agentflow/compare/v0.13.0...v0.14.0) (2026-10-29)


### Features

* **adapters:** afthreadpg, a Postgres afthread store built on sqlc ([#149](https://github.com/omniaura/agentflow/issues/149)) ([b84e1e3](https://github.com/omniaura/agentflow/commit/b84e1e33ecdf15afae4d17e828f9ced18e66be54))
* add .af line comments ([#80](https://github.com/omniaura/agentflow/issues/80)) ([4e081f7](https://github.com/omniaura/agentflow/commit/4e081f70028fa7b50e6778ed5d0c8688f15af952))
* **af:** af new scaffolds an agent backend ([#162](https://github.com/omniaura/agentflow/issues/162)) ([16079bf](https://github.com/omniaura/agentflow/commit/16079bf4111961dfc9fe3ddef5ed3367ef9d6060))
* **afcompact:** Digest condenses large tool results in parallel chunks ([#147](https://github.com/omniaura/agentflow/issues/147)) ([1ac21fa](https://github.com/omniaura/agentflow/commit/1ac21fa8436e21dc02b4ebed219375f2fc71dc94))
* **afhttp:** schema constraints validate input as 400s; SQL tools say what to fix ([#157](https://github.com/omniaura/agentflow/issues/157)) ([71dbc1e](https://github.com/omniaura/agentflow/commit/71dbc1e25e761b80ceb7adbe18a1d7473b56c8e3))
* **afopenai:** ResponsesModel runs agents on OpenAI's Responses API ([#153](https://github.com/omniaura/agentflow/issues/153)) ([bd6ceed](https://github.com/omniaura/agentflow/commit/bd6ceed67206b9deeabaeb11f967688a825744fe))
* **afthread:** follow a thread's runs over HTTP ([#160](https://github.com/omniaura/agentflow/issues/160)) ([ac72456](https://github.com/omniaura/agentflow/commit/ac7245665a5fc872900a9b7279f1241c76ecf142))
* **afthread:** Messenger lets an owner's agents message each other ([#150](https://github.com/omniaura/agentflow/issues/150)) ([09dd6b9](https://github.com/omniaura/agentflow/commit/09dd6b9dfcb2dc5883606fb1561af64d42b8f580))
* **afthreadpg:** Directory, so agents message each other on Postgres ([#152](https://github.com/omniaura/agentflow/issues/152)) ([c6ebcb7](https://github.com/omniaura/agentflow/commit/c6ebcb773c7c87a78d1e5dd22d5448536ef4ddb4))
* **afthreadpg:** Migrate applies the embedded migrations once ([#158](https://github.com/omniaura/agentflow/issues/158)) ([a8bed77](https://github.com/omniaura/agentflow/commit/a8bed77ab3f78b42886dae5af2b25302bc34d280))
* **afthread:** record runs and their events in the database ([#148](https://github.com/omniaura/agentflow/issues/148)) ([e897268](https://github.com/omniaura/agentflow/commit/e897268691773b6f5edb03ea2a02f5cd1c0d61e2))
* **afthread:** Server, the HTTP side of a chat backend ([#156](https://github.com/omniaura/agentflow/issues/156)) ([a1c5b02](https://github.com/omniaura/agentflow/commit/a1c5b026a92f86e9a735045c3244faadea8d8325))
* **afthread:** Worker runs one agent per thread across servers ([#154](https://github.com/omniaura/agentflow/issues/154)) ([ad1935e](https://github.com/omniaura/agentflow/commit/ad1935eb18bdeb215ab07acaa2d77faea00af16a))
* **agent:** .maxcalls bounds a run's tool calls ([#141](https://github.com/omniaura/agentflow/issues/141)) ([5ec48f6](https://github.com/omniaura/agentflow/commit/5ec48f64e4ffb40390c185b530f92e6d5eaea4f3))
* **agent:** .tools may name tools of other .af files in the package ([#137](https://github.com/omniaura/agentflow/issues/137)) ([7d26a2f](https://github.com/omniaura/agentflow/commit/7d26a2f6469a5a0704fd8614bec51d49533e560e))
* **agent:** admit tool batches after model responses [OSS-10] ([98b963c](https://github.com/omniaura/agentflow/commit/98b963c73c5645503d067a07b1b74d44cb1fad99))
* **agent:** Allow picks a run's tools ([#140](https://github.com/omniaura/agentflow/issues/140)) ([ef05599](https://github.com/omniaura/agentflow/commit/ef055995e73647bbd4cdad08ac2e97ae74995f3c))
* **agent:** BeforeTurn replaces the conversation before each model call ([#144](https://github.com/omniaura/agentflow/issues/144)) ([200ae89](https://github.com/omniaura/agentflow/commit/200ae899b7423c523a5c95edf8763eeadbb0e69b))
* **agent:** ForThread runs a generated agent on a thread; runs record themselves ([#155](https://github.com/omniaura/agentflow/issues/155)) ([e9469a8](https://github.com/omniaura/agentflow/commit/e9469a8d634a75f068092757b920722acea23b0f))
* **agent:** interrupt and resume tool batches [OSS-6] ([8e4c081](https://github.com/omniaura/agentflow/commit/8e4c08164df02c112bd032f2c5af8965ac8c4f87))
* **agent:** send_message and list_agents are built-in tools ([#151](https://github.com/omniaura/agentflow/issues/151)) ([aee812d](https://github.com/omniaura/agentflow/commit/aee812d1b4949b36eea39048f705f218b71c80c8))
* **agent:** subagents: .tools may name another agent ([#142](https://github.com/omniaura/agentflow/issues/142)) ([8326e27](https://github.com/omniaura/agentflow/commit/8326e27bb73fcdf67c2bfbf9700544f9e90d9cd9))
* **agent:** support dynamic tool catalogs [OSS-7] ([c00db85](https://github.com/omniaura/agentflow/commit/c00db85a43e8a0eea2f4215da5b3e75f40b2c0ff))
* **agent:** support run-specific output schemas [OSS-9] ([aedee5a](https://github.com/omniaura/agentflow/commit/aedee5ac494266635386cc552072b9404b157b8f))
* **route:** .bind reads a query argument from a request name of its own ([#163](https://github.com/omniaura/agentflow/issues/163)) ([af007ad](https://github.com/omniaura/agentflow/commit/af007ad0e20b758816f7528a78d3ce4f0ebd7683))
* **route:** .empty with a 2xx status responds with a success when no row is found ([#166](https://github.com/omniaura/agentflow/issues/166)) ([4ed33cf](https://github.com/omniaura/agentflow/commit/4ed33cf76616125b3c919531886ad5a9c8234031))
* **route:** .map funcs take all the rows, the caller or the request, and can fail ([#164](https://github.com/omniaura/agentflow/issues/164)) ([bb1acab](https://github.com/omniaura/agentflow/commit/bb1acab7f64d4c1e5fa36c82f3cd69bd7095a810))
* **route:** .prepare passes the decoded arguments to a Go func before the query ([#165](https://github.com/omniaura/agentflow/issues/165)) ([2b090fe](https://github.com/omniaura/agentflow/commit/2b090fe771f8595584c411bc15008c3a57259f7f))
* **route:** swag annotations on generated handlers ([#143](https://github.com/omniaura/agentflow/issues/143)) ([0d02b73](https://github.com/omniaura/agentflow/commit/0d02b7376081d7fbe8323521cbe0f4d48d8bc043))
* **runtime:** afcompact keeps conversations within the context window ([#146](https://github.com/omniaura/agentflow/issues/146)) ([edbe48b](https://github.com/omniaura/agentflow/commit/edbe48bb5ec011c05d6b1b1725da68c0017fa398))
* **runtime:** afthread keeps threads and inboxes in your database ([#145](https://github.com/omniaura/agentflow/issues/145)) ([93a4883](https://github.com/omniaura/agentflow/commit/93a4883a251b3bba45b1abe8c2291774b850174f))
* **survey:** count LLM gateway code that afmodel replaces ([#139](https://github.com/omniaura/agentflow/issues/139)) ([be14ef2](https://github.com/omniaura/agentflow/commit/be14ef2088a5841bc0299080a2929316e7b3a92e))


### Bug Fixes

* **agent:** validate usable dynamic tools [OSS-7] ([49b1247](https://github.com/omniaura/agentflow/commit/49b124752e979b5f67900e9b322d60c7b4d1c657))
* **swag:** describe shared-status empty response variants [DITTO-1544] ([#167](https://github.com/omniaura/agentflow/issues/167)) ([4fa5f3e](https://github.com/omniaura/agentflow/commit/4fa5f3e439619c378673973bc5844e5cc91fd7f2))

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
