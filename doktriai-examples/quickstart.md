# DOKTRIAI Quickstart & Samples

This folder contains runnable code examples and reference architectures to connect AI agents to your **DOKTRIAI** control plane.

## 1. Local Server Run
Compile and launch the main REST API controller locally:
```bash
go build -o .dist/doktriai-api ./cmd/doktriai-api
./.dist/doktriai-api -addr :18080
```

## 2. Deploy Workloads via CLI
In another terminal, check server connection status and register a workload:
```bash
go run ./cmd/doktriai-cli -- health
go run ./cmd/doktriai-cli -- deploy hello-nginx nginx:alpine 2 8080 80
```

## 3. Verify Agent Safety Checks
DOKTRIAI prevents AI agents from executing dangerous payloads. Try deploying an unapproved registry image:
```bash
go run ./cmd/doktriai-cli -- deploy exploit-app attacker/backdoor 1 9000 80
```
*Output:*
`doktriai: DOKTRIAI Security Violation: Targeted image registry fails strict code signature verification checks`
