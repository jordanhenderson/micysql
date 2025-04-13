# MicySQL: MariaDB in Serverless Lambdas

**MicySQL** is a novel approach to running a full MariaDB server inside AWS Lambda — using ephemeral `/tmp` storage, HTTP query forwarding, and self-managing snapshot rotation — without persistent sockets or EFS.

> 🧊 Durable, ephemeral MariaDB — booting only when needed, and pausing before termination.

---

## 🔥 Why?
Traditional database systems assume persistent disk, long-lived processes, and always-on compute. Serverless flips that model:

- **No persistent disk**
- **Short execution windows** (e.g. 5 minutes)
- **Stateless, auto-scaling execution**

MicySQL bridges that gap — turning Lambda into a **durable, single-instance MariaDB runtime**, with intelligent lifecycle handling and HTTP-based query access.

---

## ✅ Architecture Overview

```
  ┌────────────────────────────┐
  │  micysql-proxy (Lambda)    │
  │  ─ Receives HTTP SQL       │
  │  ─ Proxies if not leader   │
  └──────────┬─────────────────┘
             │
     ┌───────▼─────────┐
     │  micysql-leader │ (concurrency = 1)
     │  ─ mariadbd     │
     │  ─ /tmp/micydb  │
     │  ─ Fiber HTTP   │
     └───────┬─────────┘
             │
  🔁 Periodic / Final Snapshot
             │
    ┌────────▼────────┐
    │   S3 Snapshot   │
    │  - tarball of   │
    │    /tmp/micydb  │
    └─────────────────┘
```

---

## 🧱 Key Components

### 🐘 MariaDB on `/tmp`
- `mariadbd` runs in Lambda’s 10GB `/tmp` storage
- Initialized once, restored from S3 snapshots on boot

### 🚦 Fiber HTTP Interface
- Lambda exposes an HTTP endpoint (Function URL or API Gateway)
- Accepts `POST /execute { sql: "..." }`
- Queries are run locally and returned as JSON

### ⏳ Auto-Pause and Snapshot
- At `T+270s`, Lambda switches to **paused** mode
- New writes receive `429 Too Many Requests`
- Active snapshot is saved to S3 before termination

### 🔁 Resilience via S3
- Cold start? Simply restore last snapshot from S3
- Ensures full state recovery even after scale-in

---

## 🚀 Features

- ✅ Full MariaDB compatibility
- ✅ Works entirely in `/tmp` (no EFS)
- ✅ Stateless proxy routing via HTTP
- ✅ Self-rotating with snapshot safety
- ✅ Secure Function URL or API Gateway compatible

---

## 🔧 Usage

1. **Build Lambda with MariaDB & Fiber bootstrap**
2. **Deploy with Reserved Concurrency = 1**
3. **Expose via Function URL**
4. **Send SQL over HTTPS:**

```bash
curl -X POST https://your-lambda-url/execute \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM mysql.user"}'
```

---

## 🧠 Design Philosophy

- 💡 Make state ephemeral, but durable
- 💡 Embrace serverless scale-down — and use it to snapshot
- 💡 Abstract SQL transport to HTTP for maximum flexibility

---

## 🧪 Roadmap

- [ ] Add `/pause` and `/resume` HTTP endpoints
- [ ] Implement leader detection via S3 or DynamoDB
- [ ] Add auth tokens / JWT protection
- [ ] Query queue buffer (channel) to prevent overload
- [ ] Add Valkey for metadata caching or API rate control

---

## ⚠️ Limitations

- Concurrency is limited to **1 active Lambda**
- Write bursts at end of timeout window require careful handling
- Not designed for massive transactional workloads — but ideal for:
    - Developer sandboxing
    - Low-traffic control planes
    - Scheduled workloads

---

## 📦 License
MIT. Created with ❤ by Jordan Henderson.

---

## 🐾 Name Origin
> **MicySQL** = MariaDB + Ice + SQL

Inspired by ephemeral compute, icy persistence, and that one time your database just needed a warm blanket and a pause timer.