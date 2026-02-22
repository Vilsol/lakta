---
title: Installation
description: Install Lakta and set up your Go module.
---

## Requirements

- Go 1.26+
- A Go module (`go mod init`)

## Install

```bash
go get github.com/Vilsol/lakta
```

## Optional integrations

Each integration is a separate import path. Only pull in what you need:

```bash
go get github.com/Vilsol/lakta/pkg/logging/tint
go get github.com/Vilsol/lakta/pkg/logging/slog
go get github.com/Vilsol/lakta/pkg/otel
go get github.com/Vilsol/lakta/pkg/http/fiber
go get github.com/Vilsol/lakta/pkg/grpc/server
go get github.com/Vilsol/lakta/pkg/grpc/client
go get github.com/Vilsol/lakta/pkg/db/drivers/pgx
go get github.com/Vilsol/lakta/pkg/health
go get github.com/Vilsol/lakta/pkg/workflows/temporal
```
