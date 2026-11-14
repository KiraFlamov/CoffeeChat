# coffeechat

An end-to-end encrypted messenger for a small private group: a website and a PWA. The server stores and relays ciphertext only.

> Work in progress. The project is at stage 0 (scaffolding): there is no encryption or messaging yet.
> Neither this project nor the ts-mls library has had an independent security audit.

[Русская версия](README.md)

## Layout

| Directory          | Contents                                                |
| ------------------ | ------------------------------------------------------- |
| `proto/`           | Client–server contract (Protobuf)                       |
| `web/`             | Web client: React, TypeScript, Vite                     |
| `packages/crypto/` | All client-side cryptography                            |
| `packages/proto/`  | Generated TypeScript types for the contract             |
| `server/`          | Go server                                               |
| `deploy/`          | Infrastructure: Terraform, docker-compose, gateway spec |

## Requirements

- Go 1.27 or newer
- Node.js 22 or newer, and pnpm
- [Task](https://taskfile.dev) to run project commands (on Windows: `winget install Task.Task`)
- [golangci-lint](https://golangci-lint.run) 2.x

## Commands

```
pnpm install     # TypeScript dependencies
task lint        # Go and TypeScript linters, formatting check
task test        # all tests
task fmt         # format code
```

## License

[MIT](LICENSE)
