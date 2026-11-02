# Contributing

Thanks for considering a contribution.

- Setup, environment variables and checks: see this repository's README.
- This repository is an independently built and deployed unit of the 0xbot platform. Do not import runtime source from sibling projects; cross-project changes go through the versioned OpenAPI contract (`internal/contract/openapi.json` in [0xbot-api](https://github.com/0xbot-studio/0xbot-api)). Frontend clients are generated from that contract, not hand-maintained.
- Run lint / typecheck / test / build before opening a pull request; CI runs the same checks on every push.
- Bug fixes and small improvements can go straight to a PR. For new capabilities or behavior changes, open an issue first to discuss the design — capability identity, versions and REST/MCP mappings are contractual. Cross-module decisions are recorded as ADRs in [0xbot-studio/0xbot](https://github.com/0xbot-studio/0xbot).
- By contributing, you agree that your contributions are licensed under Apache-2.0.
